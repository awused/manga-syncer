package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

const mangaURL = "https://api.mangadex.org/v2/manga/%s?include=chapters"

type mangaChapter struct {
	ID         int    `json:"id"`
	Hash       string `json:"hash"`
	MangaID    int    `json:"mangaId"`
	MangaTitle string `json:"mangaTitle"`
	Volume     string `json:"volume"`
	Chapter    string `json:"chapter"`
	Title      string `json:"title"`
	Language   string `json:"language"`
	Groups     []int  `json:"groups"`
	Uploader   int    `json:"uploader"`
	Timestamp  int    `json:"timestamp"`
	ThreadID   int    `json:"threadId"`
	Comments   int    `json:"comments"`
	Views      int    `json:"views"`
}

type mangaMetadata struct {
	Code   int    `json:"code"`
	Status string `json:"status"`
	Data   struct {
		Manga struct {
			ID          int      `json:"id"`
			Title       string   `json:"title"`
			AltTitles   []string `json:"altTitles"`
			Description string   `json:"description"`
			Artist      []string `json:"artist"`
			Author      []string `json:"author"`
			Publication struct {
				Language    string `json:"language"`
				Status      int    `json:"status"`
				Demographic int    `json:"demographic"`
			} `json:"publication"`
			Tags        []int       `json:"tags"`
			LastChapter interface{} `json:"lastChapter"`
			LastVolume  interface{} `json:"lastVolume"`
			IsHentai    bool        `json:"isHentai"`
			Links       struct {
				Al    string `json:"al"`
				Ap    string `json:"ap"`
				Bw    string `json:"bw"`
				Kt    string `json:"kt"`
				Mu    string `json:"mu"`
				Amz   string `json:"amz"`
				Ebj   string `json:"ebj"`
				Mal   string `json:"mal"`
				Raw   string `json:"raw"`
				Engtl string `json:"engtl"`
			} `json:"links"`
			Relations []interface{} `json:"relations"`
			Rating    struct {
				Bayesian float64 `json:"bayesian"`
				Mean     float64 `json:"mean"`
				Users    int     `json:"users"`
			} `json:"rating"`
			Views        int    `json:"views"`
			Follows      int    `json:"follows"`
			Comments     int    `json:"comments"`
			LastUploaded int    `json:"lastUploaded"`
			MainCover    string `json:"mainCover"`
		} `json:"manga"`
		Chapters []mangaChapter `json:"chapters"`
		Groups   []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"groups"`
	} `json:"data"`
}

func getOrCreateMangaDirectory(m mangaMetadata, mid string) (string, error) {
	dirs, err := ioutil.ReadDir(conf.OutputDirectory)
	if err != nil {
		return "", err
	}

	// This will break and die if an existing non-dir file exists
	existing := findExisting(dirs, mid)
	if existing != "" {
		return filepath.Join(conf.OutputDirectory, existing), nil
	}

	dirName := convertName(m.Data.Manga.Title) + " - " + mid
	dir := filepath.Join(conf.OutputDirectory, dirName)
	return dir, os.Mkdir(dir, 0755)
}

func buildChapterArchiveName(c mangaChapter, cid string, groups map[int]string) string {
	out := ""
	if c.Volume != "" {
		out += "Vol. " + c.Volume + " "
	}

	out += "Ch. " + c.Chapter

	if c.Title != "" {
		out += " " + c.Title
	}

	gns := []string{}
	for _, g := range c.Groups {
		gn, ok := groups[g]
		if ok {
			gns = append(gns, gn)
		}
	}
	out += " [" + strings.Join(gns, ", ") + "]"

	out += " - " + cid + ".zip"
	return convertName(out)
}

func syncManga(mid string, ch chan<- chapterJob) {
	resp, err := client.Get(fmt.Sprintf(mangaURL, mid))
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err)
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorln("Manga "+mid, resp.Request.URL, errors.New(resp.Status), string(body))
		return
	}

	var m mangaMetadata
	err = json.Unmarshal(body, &m)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err, string(body))
		return
	}

	if m.Code != 200 {
		log.Errorln("Manga "+mid, resp.Request.URL, errors.New(m.Status), string(body))
		return
	}

	mangaDir, err := getOrCreateMangaDirectory(m, mid)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err)
		return
	}

	files, err := ioutil.ReadDir(mangaDir)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err)
		return
	}

	archives := []os.FileInfo{}
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".zip" {
			archives = append(archives, f)
		}
	}

	groups := make(map[int]string)
	for _, g := range m.Data.Groups {
		groups[g.ID] = g.Name
	}

	for _, c := range m.Data.Chapters {
		cid := strconv.Itoa(c.ID)
		if c.Language != conf.Language {
			continue
		}

		if findExisting(archives, cid) != "" {
			continue
		}

		fileName := buildChapterArchiveName(c, cid, groups)
		filePath := filepath.Join(mangaDir, fileName)
		select {
		case ch <- chapterJob{
			chapterID:   cid,
			archivePath: filePath,
		}:
		case <-closeChan:
			return
		}
	}
}
