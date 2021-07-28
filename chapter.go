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
	chapter     mangaChapter
	archivePath string
}

type atHomeResponse struct {
	BaseURL string `json:"baseUrl"`
}

const atHomeServerURL = "https://api.mangadex.org/at-home/server/%s"

// This one has a hard 1/s limit, so only consume half of it
var atHomeTicker = time.NewTicker(time.Second * 2)

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

	if *chapterFlag == "" {
		select {
		case <-closeChan:
			return
		case <-atHomeTicker.C:
		}
	}

	resp, err := client.Get(fmt.Sprintf(atHomeServerURL, c.chapter.Data.ID))
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
		log.Errorln("Chapter "+c.chapter.Data.ID, resp.Request.URL, errors.New(resp.Status), string(body))
		return
	}

	var ah atHomeResponse
	err = json.Unmarshal(body, &ah)
	if err != nil {
		log.Errorln(err)
		return
	}

	if ah.BaseURL == "" {
		log.Errorln("Chapter "+c.chapter.Data.ID, resp.Request.URL, "Empty base URL")
		return
	}

	errCh := make(chan error)
	for i, p := range c.chapter.Data.Attributes.Data {
		select {
		case <-closeChan:
			return
			// case <-time.After(delay):
		default:
		}

		url := ah.BaseURL + "/data/" + c.chapter.Data.Attributes.Hash + "/" + p
		file := filepath.Join(dir, fmt.Sprintf("%03d", i+1)+filepath.Ext(p))
		go func() {
			select {
			case <-closeChan:
				errCh <- errors.New("closed")
				return
				// case <-time.After(delay):
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			err := downloadImage(url, file)
			if err != nil {
				log.Errorln("Chapter "+c.chapter.Data.ID, url, err)
			}
			errCh <- err
		}()
	}

	for range c.chapter.Data.Attributes.Data {
		pageErr := <-errCh
		if pageErr != nil {
			err = pageErr
		}
	}
	close(errCh)
	if err != nil {
		return
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
