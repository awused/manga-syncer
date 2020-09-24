package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

const mangaURL = "https://mangadex.org/api/?id=%s&server=null&saver=0&type=manga"

type mangaChapter struct {
	Volume     string `json:"volume"`
	Chapter    string `json:"chapter"`
	Title      string `json:"title"`
	LangName   string `json:"lang_name"`
	LangCode   string `json:"lang_code"`
	GroupID    int    `json:"group_id"`
	GroupName  string `json:"group_name"`
	GroupID2   int    `json:"group_id_2"`
	GroupName2 string `json:"group_name_2"`
	GroupID3   int    `json:"group_id_3"`
	GroupName3 string `json:"group_name_3"`
	Timestamp  int    `json:"timestamp"`
	Comments   int    `json:"comments"`
}

type mangaMetadata struct {
	Manga struct {
		CoverURL    string   `json:"cover_url"`
		Description string   `json:"description"`
		Title       string   `json:"title"`
		AltNames    []string `json:"alt_names"`
		Artist      string   `json:"artist"`
		Author      string   `json:"author"`
		Status      int      `json:"status"`
		Genres      []int    `json:"genres"`
		LastChapter string   `json:"last_chapter"`
		LangName    string   `json:"lang_name"`
		LangFlag    string   `json:"lang_flag"`
		Hentai      int      `json:"hentai"`
		Links       struct {
			Al  string `json:"al"`
			Ap  string `json:"ap"`
			Kt  string `json:"kt"`
			Mu  string `json:"mu"`
			Amz string `json:"amz"`
			Ebj string `json:"ebj"`
			Mal string `json:"mal"`
			Raw string `json:"raw"`
		} `json:"links"`
		Rating struct {
			Bayesian string `json:"bayesian"`
			Mean     string `json:"mean"`
			Users    string `json:"users"`
		} `json:"rating"`
	} `json:"manga"`
	Chapter map[string]mangaChapter `json:"chapter"`
	Group   map[string]struct {
		GroupName string `json:"group_name"`
	} `json:"group"`
	Status string `json:"status"`
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

	dirName := convertName(m.Manga.Title) + " - " + mid
	dir := filepath.Join(conf.OutputDirectory, dirName)
	return dir, os.Mkdir(dir, 0755)
}

func buildChapterArchiveName(c mangaChapter, cid string) string {
	out := ""
	if c.Volume != "" {
		out += "Vol. " + c.Volume + " "
	}

	out += "Ch. " + c.Chapter

	if c.Title != "" {
		out += " " + c.Title
	}

	if c.GroupName != "" {
		out += " [" + c.GroupName

		if c.GroupName2 != "" {
			out += ", " + c.GroupName2
		}

		if c.GroupName3 != "" {
			out += ", " + c.GroupName3
		}

		out += "]"
	}

	out += " - " + cid + ".zip"
	return convertName(out)
}

func syncManga(mid string, ch chan<- chapterJob) {
	resp, err := client.Get(fmt.Sprintf(mangaURL, mid))
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

	var m mangaMetadata
	err = json.Unmarshal(body, &m)
	if err != nil {
		log.Errorln(err)
		return
	}

	mangaDir, err := getOrCreateMangaDirectory(m, mid)
	if err != nil {
		log.Errorln(err)
		return
	}

	files, err := ioutil.ReadDir(mangaDir)
	if err != nil {
		log.Errorln(err)
		return
	}

	archives := []os.FileInfo{}
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".zip" {
			archives = append(archives, f)
		}
	}

	for cid, c := range m.Chapter {
		if c.LangCode != conf.Language {
			continue
		}

		if findExisting(archives, cid) != "" {
			continue
		}

		fileName := buildChapterArchiveName(c, cid)
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
