package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"github.com/caddyserver/certmagic"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"glacier/config"
	"glacier/autoclean"
	"glacier/prometheus"
)
/*
func getDiskUsageAllowed() float64 {
	allowedDiskEnv, err := strconv.ParseFloat(os.Getenv("DISK_USAGE_ALLOWED"), 64)
	if err != nil {
		allowedDiskEnv = 75
	}
	if allowedDiskEnv < 5 || allowedDiskEnv > 99 {
		fmt.Println("Panic! DISK_USAGE_ALLOWED not accepted. Only 5-99 is allowed")
		allowedDiskEnv = 75
	}
	fmt.Println("DISK_USAGE_ALLOWED:", allowedDiskEnv, "%")
	return allowedDiskEnv
}

var DiskUsageAllowed = getDiskUsageAllowed()
*/
func ExtractGUID() *regexp.Regexp {
	r, err := regexp.Compile("(20[0-9]{6}-[0-9]{2}[a-f0-9]{2}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})")
	if err != nil {
		panic(err)
	}
	return r
}

var extractGUID = ExtractGUID()

var ctx = context.Background()

func getContainerFile(uuidString string) (string, string, error) {
	timeUuid := extractGUID.FindString(uuidString)
	id, err := uuid.Parse(timeUuid)

	if err != nil {
		return "", timeUuid, err
	}

	if id.Version() != 4 {
		return "", timeUuid, errors.New("UUID not version 4")
	}
	if timeUuid[0:2] != "20" {
		return "", timeUuid, errors.New("UUID not time-uuid")
	}

	keep, err := strconv.ParseUint(timeUuid[11:13], 16, 64)
	if err != nil {
		keep = 0
		fmt.Println("strconv.ParseUint:", err)
	}
	//fmt.Println("uint8:", uint8(keep))
	if config.Settings.Get(config.EXTEND_LIFE_SUPPORT) == "true" && uint8(keep)&0x80 == 0x80 {
		keep = uint64(uint8(keep) & 0x7f)
		TimeLayout := "20060102"
		timestamp, err := time.Parse(TimeLayout, timeUuid[0:8])
		if err != nil {
			return "", timeUuid, errors.New("UUID unable to pase timeUuid:" + timeUuid)
		}
		//fmt.Println(timestamp)
		timestamp = timestamp.AddDate(0, int(keep), 0)
		idString := timestamp.Format("20060102")
		//fmt.Println(timestamp)
		return "files/" + idString[0:4] + "/" + idString[4:6] + "/" + idString[6:8] + "/" + timeUuid[9:11] + "/" + timeUuid[34:36] + ".tar", timeUuid, err
	} else {
		return "files/" + timeUuid[0:4] + "/" + timeUuid[4:6] + "/" + timeUuid[6:8] + "/" + timeUuid[9:11] + "/" + timeUuid[34:36] + ".tar", timeUuid, err
	}

}

func getFile(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		fmt.Println("id is missing in parameters")
	}
	fmt.Println(id)

	containerFile, id, err := getContainerFile(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		fmt.Println(err)
		return
	}
	fileLock := flock.New(containerFile)
	locked, err := fileLock.TryLockContext(ctx, 500*time.Millisecond)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		fmt.Println("lock timeout:", err)
		return
	}
	if !locked {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Println("file not locked:")
		return
	}
	defer fileLock.Unlock()
	tarFile, err := os.Open(containerFile)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "open tar file failed", err)
		return
	}
	defer tarFile.Close()

	tr := tar.NewReader(tarFile)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, "File not found")
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "open tar file failed", err)
			return
		}
		if id == hdr.Name {
			if len(hdr.Gname) > 0 {
				w.Header().Set("Content-Type", hdr.Gname)
			}
			if hdr.Mode == int64(1) {
				gzf, err := gzip.NewReader(tr)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(w, "gzip decompress error:", err)
					return
				}
				defer gzf.Close()
				_, err = io.Copy(w, gzf)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(w, "open compressed tar file failed", err)
					fmt.Println("open compressed file failed", err)
					return
				}
			} else {
				if _, err := io.Copy(w, tr); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(w, "open tar file failed", err)
					return
				}
			}
			return
		}
	}
}

func sharedUpload(w http.ResponseWriter, r *http.Request, id string, fileBytes []byte) {
	containerFile, uuid_id, err := getContainerFile(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		fmt.Println(err)
		return
	}
	containerPath := filepath.Dir(containerFile)

	err = os.MkdirAll(containerPath, 0700)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		return
	}

	if len(fileBytes) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "FileSize==0")
		fmt.Println("FileSize==0")
		return
	}
	mtype := mimetype.Detect(fileBytes)
	doCompress := false
	if len(fileBytes) > 100 { // Pointless compressing small blobs as GZIP-header will create big overhead
		_, doCompress = DO_COMPRESS[mtype.Extension()]
		if !doCompress {
			_, doCompress = DO_COMPRESS[mtype.String()]
		}
	}
	if len(fileBytes) < 20000000 && len(fileBytes) > 100 {
		doCompress = true
	}

	var output bytes.Buffer
	if doCompress { // Compress content
		gw := gzip.NewWriter(&output)

		_, err = gw.Write(fileBytes)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			return
		}
		if err := gw.Close(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			fmt.Println("compress close failed:", err)
			return
		}
	}

	fileLock := flock.New(containerFile)
	locked, err := fileLock.TryLockContext(ctx, 500*time.Millisecond)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		fmt.Println("lock timeout:", err)
		return
	}
	if !locked {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Println("file not locked:")
		return
	}
	defer fileLock.Unlock()

	prometheus.Tar_files_open.Inc()
	defer prometheus.Tar_files_open.Dec()

	f, err := os.OpenFile(containerFile, os.O_RDWR|os.O_CREATE, os.ModePerm)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		return
	}
	defer f.Close()
	if _, err = f.Seek(-2<<9, os.SEEK_END); err != nil {
		fmt.Println(err)
	}

	tw := tar.NewWriter(f)

	if !doCompress {
		hdr := &tar.Header{
			Name:   uuid_id,
			Size:   int64(len(fileBytes)),
			Format: tar.FormatPAX,
			Uname:  id,
			Gname:  mtype.String(),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			return
		}

		if _, err := tw.Write(fileBytes); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			return
		}
	} else {
		hdr := &tar.Header{
			Name:   uuid_id,
			Size:   int64(output.Len()),
			Uid:    int(len(fileBytes)),
			Format: tar.FormatPAX,
			Uname:  id,
			Gname:  mtype.String(),
			Mode:   1, //Define we use compression
		}

		if err := tw.WriteHeader(hdr); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			return
		}
		if _, err := io.Copy(tw, &output); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err)
			return
		}
	}

	if err := tw.Close(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	prometheus.RawUploadDoneProcessed.Inc()
	fmt.Fprintf(w, "<html><a href=get/%v>%v</a> <br><a href=%v>%v</a>", id, id, containerFile, containerFile)

}

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
	sharedUpload(w, r, id, fileBytes)

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
		fileUUID = generateTimeUUID()
	}
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err)
		return
	}
	sharedUpload(w, r, fileUUID, fileBytes)
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

func generateTimeUUID() string {
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
	idString := generateTimeUUID()
	containerFile, idString, _ := getContainerFile(idString)
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
		fmt.Fprintf(w, "<table class=\"table table-hover\"><tr><th>ID</th><th>Name</th><th>GzipSize</th><th>RealSize</th><th>GzipRatio</th><th>Filename</th><th>MimeType</th><th>Compressed</th><tr>")
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
			fmt.Fprintf(w, "<tr><td>%d</td><td><a href=..\\..\\..\\..\\..\\get\\%v>%v</a></td><td>%d</td><td>%d</td><td>%.2f</td><td>%v</td><td>%v</td><td>%v</td<<tr>", count, hdr.Name, hdr.Name, hdr.Size, hdr.Uid, (float64)((float64)(hdr.Size)/(float64)(hdr.Uid)), hdr.Uname, hdr.Gname, hdr.Mode)
			count = count + 1
		}
		fmt.Fprintf(w, "</table>")
	})
}

func main() {
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
		return
	}
	go autoclean.AutoClean()
	go prometheus.SystemStat()
	r := mux.NewRouter()
	filesfs := http.StripPrefix("/files/", http.FileServer(http.Dir(config.Settings.Get(config.DATA_FOLDER))))
	r.PathPrefix("/files/").Handler(FileView(filesfs))
	staticfs := http.StripPrefix("/static/", http.FileServer(http.Dir("/static")))
	r.PathPrefix("/static/").Handler(FileView(staticfs))

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "/static/index.html")
	})

	r.HandleFunc("/uuid", uuidhello)
	r.HandleFunc("/upload", uploadFile)
	r.HandleFunc("/rawupload/{id}", rawUpload)
	r.HandleFunc("/get/{id}", getFile)
	r.HandleFunc("/redirect", redirect)

	r.Handle("/metrics", promhttp.Handler())

	if config.Settings.Has(config.SERVER_DOMAIN) && config.Settings.Has(config.ACME_SERVER) {
		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.CA = config.Settings.Get(config.ACME_SERVER)
		log.Fatal(certmagic.HTTPS([]string{config.Settings.Get(config.SERVER_DOMAIN)}, r))
	} else {
		http.ListenAndServe(":8000", r)
	}
}
