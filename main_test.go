package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestServer(t *testing.T) {
	test_uuid := "20221005-1123-4816-a6fa-2b0afbe67452"
	data := []byte("this is some data stored as a byte slice in Go Lang!")
	r := InitServer()
	go http.ListenAndServe(":8000", r)

	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	fileWriter, err := bodyWriter.CreateFormFile("myFile", test_uuid)
	if err != nil {
		fmt.Println("error writing to buffer")
	}

	reader := bytes.NewReader(data)
	io.Copy(fileWriter, reader)
	contentType := bodyWriter.FormDataContentType()
	bodyWriter.Close()
	resp, err := http.Post("http://localhost:8000/upload", contentType, bodyBuf)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("Wrong response-code! Have:\"%v\"", resp.Status )
	}

	getresp, err := http.Get("http://localhost:8000/get/" + test_uuid)
	if err != nil {
		log.Fatalln(err)
	}

	getbody, err := ioutil.ReadAll(getresp.Body)
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(string(getbody))

	if bytes.Compare(getbody, data) != 0 {
		t.Fatalf("Upload/download did not pass! Want:\"%v\" Have:\"%v\"", string(data), string(getbody) )
	}
}
