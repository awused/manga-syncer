package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type chapterJob struct {
	chapterID   string
	archivePath string
}

type chapterMetadata struct {
	Code   int    `json:"code"`
	Status string `json:"status"`
	Data   struct {
		ID         int    `json:"id"`
		Hash       string `json:"hash"`
		MangaID    int    `json:"mangaId"`
		MangaTitle string `json:"mangaTitle"`
		Volume     string `json:"volume"`
		Chapter    string `json:"chapter"`
		Title      string `json:"title"`
		Language   string `json:"language"`
		Groups     []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"groups"`
		Uploader       int      `json:"uploader"`
		Timestamp      int      `json:"timestamp"`
		ThreadID       int      `json:"threadId"`
		Comments       int      `json:"comments"`
		Views          int      `json:"views"`
		Status         string   `json:"status"`
		Pages          []string `json:"pages"`
		Server         string   `json:"server"`
		ServerFallback string   `json:"serverFallback"`
	} `json:"data"`
}

const chapterURL = "https://api.mangadex.org/v2/chapter/%s?server=null&saver=0"

func downloadImage(url string, file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New(resp.Status)
	}

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

func downloadChapter(c chapterJob) {
	log.Debugln("Started downloading: " + c.archivePath)

	dir, err := ioutil.TempDir(conf.TempDirectory, "manga-syncer")
	if err != nil {
		log.Errorln(err)
		return
	}
	defer os.RemoveAll(dir)

	resp, err := client.Get(fmt.Sprintf(chapterURL, c.chapterID))
	if err != nil {
		log.Errorln(err)
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorln(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorln("Chapter "+c.chapterID, resp.Request.URL, errors.New(resp.Status), string(body))
		return
	}

	var cm chapterMetadata
	err = json.Unmarshal(body, &cm)
	if err != nil {
		log.Errorln(err)
		return
	}

	if cm.Code != 200 {
		log.Errorln("Chapter "+c.chapterID, resp.Request.URL, errors.New(cm.Status), string(body))
		return
	}

	for i, p := range cm.Data.Pages {
		select {
		case <-closeChan:
			return
		case <-time.After(5 * time.Second):
		}

		url := cm.Data.Server + cm.Data.Hash + "/" + p
		file := filepath.Join(dir, fmt.Sprintf("%03d", i+1)+filepath.Ext(p))
		err = downloadImage(url, file)
		if err != nil {
			log.Errorln("Chapter "+c.chapterID, url, err)
			return
		}
	}

	out, err := exec.Command("zip", "-j", "-r", c.archivePath, dir).CombinedOutput()
	if err != nil {
		log.Println("Error zipping directory: " + string(out))
		log.Errorln(err)
		return
	}

	log.Debugln("Finished downloading: " + c.archivePath)
}

func chapterWorker(ch <-chan chapterJob, wg *sync.WaitGroup) {
	defer wg.Done()

	for c := range ch {
		select {
		case <-closeChan:
			return
		default:
		}

		downloadChapter(c)
	}
}
