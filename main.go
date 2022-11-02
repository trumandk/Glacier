package main

import (
	"bytes"
	"context"
	"fmt"
	"glacier/autoclean"
	"glacier/config"
	"glacier/gui"
	"glacier/prometheus"
	"glacier/s3"
	"glacier/shared"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/caddyserver/certmagic"
	"github.com/gabriel-vasile/mimetype"
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
		fileUUID = shared.GenerateTimeUUID()
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
	r.PathPrefix("/files/").Handler(gui.FileView(filesfs))
	staticfs := http.StripPrefix("/static/", http.FileServer(http.Dir("/static")))
	r.PathPrefix("/static/").Handler(staticfs)

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

	r.HandleFunc("/uuid", gui.Uuidhello)
	r.HandleFunc("/uuidv1", gui.Uuidv1hello)
	r.HandleFunc("/data/", s3.S3Bucket)
	r.HandleFunc("/data/{id}", s3.S3Put)
	r.HandleFunc("/upload", uploadFile)
	r.HandleFunc("/rawupload/{id}", rawUpload)
	r.HandleFunc("/get/{id}", shared.GetFile)
	r.HandleFunc("/redirect", gui.Redirect)

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
