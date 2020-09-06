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
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"
)

type chapterJob struct {
	chapterID   string
	archivePath string
}

type chapterMetadata struct {
	ID         int         `json:"id"`
	Timestamp  int         `json:"timestamp"`
	Hash       string      `json:"hash"`
	Volume     string      `json:"volume"`
	Chapter    string      `json:"chapter"`
	Title      string      `json:"title"`
	LangName   string      `json:"lang_name"`
	LangCode   string      `json:"lang_code"`
	MangaID    int         `json:"manga_id"`
	GroupID    int         `json:"group_id"`
	GroupName  string      `json:"group_name"`
	GroupID2   int         `json:"group_id_2"`
	GroupName2 interface{} `json:"group_name_2"`
	GroupID3   int         `json:"group_id_3"`
	GroupName3 interface{} `json:"group_name_3"`
	Comments   int         `json:"comments"`
	Server     string      `json:"server"`
	PageArray  []string    `json:"page_array"`
	Status     string      `json:"status"`
	// Was int, now bool, but not not relevant
	//LongStrip  bool				`json:"long_strip"`
}

const chapterURL = "https://mangadex.org/api/?id=%s&server=null&saver=0&type=chapter"

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
		log.Errorln(errors.New(resp.Status))
		return
	}

	var cm chapterMetadata
	err = json.Unmarshal(body, &cm)
	if err != nil {
		log.Errorln(err)
		return
	}

	for i, p := range cm.PageArray {
		select {
		case <-closeChan:
			return
		default:
		}

		url := cm.Server + cm.Hash + "/" + p
		file := filepath.Join(dir, strconv.Itoa(i+1)+filepath.Ext(p))
		err = downloadImage(url, file)
		if err != nil {
			log.Errorln(err)
			return
		}
	}

	err = exec.Command("zip", "-j", "-r", c.archivePath, dir).Run()
	if err != nil {
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
