package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"glacier/autoclean"
	"glacier/config"
	"glacier/prometheus"
	"glacier/s3"
	"glacier/shared"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var ctx = context.Background()

func rawUpload(w http.ResponseWriter, r *http.Request) {
	prometheus.RawUploadProcessed.Inc()
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		fmt.Println("id is missing in parameters")
	}
	fileBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		return
	}
	defer r.Body.Close()
	id, containerFile := shared.SharedUpload(w, r, id, fileBytes)
	fmt.Fprintf(w, "<html><a href=get/%v>%v</a> <br><a href=%v>%v</a>", id, id, containerFile, containerFile)

}

func uploadFile(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.ParseMultipartForm(10 << 30)
	file, handler, err := r.FormFile("myFile")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "Error Retrieving the File")
		fmt.Fprintln(w, err)
		fmt.Println("Error Retrieving the File", err)
		return
	}
	defer file.Close()
	fileUUID := handler.Filename
	generateNewUUID := r.URL.Query().Get("newuuid")
	if generateNewUUID != "" {
		fileUUID = GenerateTimeUUID()
	}
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		return
	}
	id, containerFile := shared.SharedUpload(w, r, fileUUID, fileBytes)
	fmt.Fprintf(w, "<html><a href=get/%v>%v</a> <br><a href=%v>%v</a>", id, id, containerFile, containerFile)
}

func redirect(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("myFile")
	if id != "" {
		http.Redirect(w, r, "get/"+id, http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintln(w, "Error Retrieving the File")
}

func GenerateTimeUUID() string {
	id, _ := uuid.NewRandom()
	timeStamp := time.Now()
	idString := timeStamp.Format("20060102-1504") + id.String()[13:]
	return idString
}

type MyUUID struct {
	Uuid          string
	ContainerFile string
}

func uuidhello(w http.ResponseWriter, req *http.Request) {
	idString := GenerateTimeUUID()
	containerFile, idString, _ := shared.GetContainerFile(idString)
	myuuid := &MyUUID{Uuid: idString, ContainerFile: containerFile}
	jData, err := json.Marshal(myuuid)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jData)
	}
}
func uuidv1hello(w http.ResponseWriter, req *http.Request) {
	uuidv1, _ := uuid.NewUUID()
	idString := uuidv1.String()
	containerFile, idString, _ := shared.GetContainerFile(idString)
	myuuid := &MyUUID{Uuid: idString, ContainerFile: containerFile}
	jData, err := json.Marshal(myuuid)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jData)
	}
}

func FileView(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := 0
		fileExt := path.Ext(r.URL.String())
		if fileExt != ".tar" {
			next.ServeHTTP(w, r)
			return
		}
		tarFile, err := os.Open("." + r.URL.String())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "open tar file failed", err)
			return
		}
		defer tarFile.Close()

		tr := tar.NewReader(tarFile)
		fmt.Fprintf(w, "<html><head><link href=../../../../../static/bootstrap.css rel=stylesheet></head>")
		fmt.Fprintf(w, "<table class=\"table table-hover\"><tr><th>ID</th><th>Name</th><th>GzipSize</th><th>RealSize</th><th>GzipRatio</th><th>Filename</th><th>MimeType</th><th>Compressed</th><th>Metadata</th><tr>")
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, "open tar file failed", err)
				break
			}
			metadata, _ := json.Marshal(hdr.PAXRecords)
			fmt.Fprintf(w, "<tr><td>%d</td><td><a href=..\\..\\..\\..\\..\\get\\%v>%v</a></td><td>%d</td><td>%d</td><td>%.2f</td><td>%v</td><td>%v</td><td>%v</td><td>%v</td><tr>", count, hdr.Name, hdr.Name, hdr.Size, hdr.Uid, (float64)((float64)(hdr.Size)/(float64)(hdr.Uid)), hdr.Uname, hdr.Gname, hdr.Mode, string(metadata))
			count = count + 1
		}
		fmt.Fprintf(w, "</table>")
	})
}

func InitServer() *mux.Router {
	config.Settings.Init()
	pcapDetector := func(raw []byte, limit uint32) bool {
		return bytes.HasPrefix(raw, []byte("\xd4\xc3\xb2\xa1"))
	}
	mimetype.Extend(pcapDetector, "application/pcap", ".pcap")
	pcapngDetector := func(raw []byte, limit uint32) bool {
		return bytes.HasPrefix(raw, []byte("\x0a\x0d\x0d\x0a"))
	}
	mimetype.Extend(pcapngDetector, "application/x-pcapng", ".pcapng")

	err := os.MkdirAll(config.Settings.Get(config.DATA_FOLDER), 0700)
	if err != nil {
		fmt.Println(err)
		log.Fatal("Panic unable to create folder:", config.Settings.Get(config.DATA_FOLDER))
	}
	r := mux.NewRouter()
	filesfs := http.StripPrefix("/files/", http.FileServer(http.Dir(config.Settings.Get(config.DATA_FOLDER))))
	r.PathPrefix("/files/").Handler(FileView(filesfs))
	staticfs := http.StripPrefix("/static/", http.FileServer(http.Dir("/static")))
	r.PathPrefix("/static/").Handler(FileView(staticfs))

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		/*
			authorization := r.Header.Get("Authorization")

			fmt.Println(authorization)
			fmt.Println(r.URL)
			fmt.Println(r)
			for k, v := range r.Header {
				fmt.Printf("%v: %v\n", k, v)
			}*/
		http.ServeFile(w, r, "/static/index.html")
	})
	r.HandleFunc("/data2/", func(w http.ResponseWriter, r *http.Request) {

		authorization := r.Header.Get("Authorization")

		fmt.Println(authorization)
		fmt.Println(r.URL)
		fmt.Println(r)
		for k, v := range r.Header {
			fmt.Printf("%v: %v\n", k, v)
		}
	})

	r.HandleFunc("/uuid", uuidhello)
	r.HandleFunc("/uuidv1", uuidv1hello)
	r.HandleFunc("/data/", s3.S3Bucket)
	r.HandleFunc("/data/{id}", s3.S3Put)
	r.HandleFunc("/upload", uploadFile)
	r.HandleFunc("/rawupload/{id}", rawUpload)
	r.HandleFunc("/get/{id}", shared.GetFile)
	r.HandleFunc("/redirect", redirect)

	r.Handle("/metrics", promhttp.Handler())
	return r
}

func main() {
	r := InitServer()
	go autoclean.AutoClean()
	go prometheus.SystemStat()

	if config.Settings.Has(config.SERVER_DOMAIN) && config.Settings.Has(config.ACME_SERVER) {
		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.CA = config.Settings.Get(config.ACME_SERVER)
		log.Fatal(certmagic.HTTPS([]string{config.Settings.Get(config.SERVER_DOMAIN)}, r))
	} else {
		http.ListenAndServe(":"+config.Settings.Get(config.SERVER_PORT), r)
	}
}
