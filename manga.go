package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const pageSize = 100

const mangaURL = "https://api.mangadex.org/manga/%s"
const scanlationGroupsURL = "https://api.mangadex.org/group?limit=100"

type mangaChapter struct {
	Result string `json:"result"`
	Data   struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Volume             *stringable `json:"volume"`
			Chapter            stringable  `json:"chapter"`
			Title              string      `json:"title"`
			TranslatedLanguage string      `json:"translatedLanguage"`
			Hash               string      `json:"hash"`
			Data               []string    `json:"data"`
			DataSaver          []string    `json:"dataSaver"`
			PublishAt          time.Time   `json:"publishAt"`
			CreatedAt          time.Time   `json:"createdAt"`
			UpdatedAt          interface{} `json:"updatedAt"`
			Version            int         `json:"version"`
		} `json:"attributes"`
	} `json:"data"`
	Relationships []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"relationships"`
}

type chaptersResponse struct {
	Results []mangaChapter `json:"results"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	Total   int            `json:"total"`
}

type mangaMetadata struct {
	Result string `json:"result"`
	Data   struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Title     map[string]string `json:"title"`
			AltTitles []struct {
				En string `json:"en"`
			} `json:"altTitles"`
			Description struct {
				En string `json:"en"`
			} `json:"description"`
			IsLocked bool `json:"isLocked"`
			Links    struct {
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
			OriginalLanguage       string      `json:"originalLanguage"`
			LastVolume             interface{} `json:"lastVolume"`
			LastChapter            string      `json:"lastChapter"`
			PublicationDemographic string      `json:"publicationDemographic"`
			Status                 string      `json:"status"`
			Year                   interface{} `json:"year"`
			ContentRating          string      `json:"contentRating"`
			Tags                   []struct {
				ID         string `json:"id"`
				Type       string `json:"type"`
				Attributes struct {
					Name struct {
						En string `json:"en"`
					} `json:"name"`
					Version int `json:"version"`
				} `json:"attributes"`
			} `json:"tags"`
			CreatedAt time.Time   `json:"createdAt"`
			UpdatedAt interface{} `json:"updatedAt"`
			Version   int         `json:"version"`
		} `json:"attributes"`
	} `json:"data"`
	Relationships []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"relationships"`
}

type scanlationGroups struct {
	Results []struct {
		Result string `json:"result"`
		Data   struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				Name   string `json:"name"`
				Leader struct {
					ID         string `json:"id"`
					Type       string `json:"type"`
					Attributes struct {
						Username string `json:"username"`
						Version  int    `json:"version"`
					} `json:"attributes"`
				} `json:"leader"`
				Members []struct {
					ID         string `json:"id"`
					Type       string `json:"type"`
					Attributes struct {
						Username string `json:"username"`
						Version  int    `json:"version"`
					} `json:"attributes"`
				} `json:"members"`
				CreatedAt time.Time   `json:"createdAt"`
				UpdatedAt interface{} `json:"updatedAt"`
				Version   int         `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
		Relationships []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"relationships"`
	} `json:"results"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func getOrCreateMangaDirectory(m mangaMetadata, mUUID string) (string, error) {
	dirs, err := ioutil.ReadDir(conf.OutputDirectory)
	if err != nil {
		return "", err
	}

	mid, err := convertUUID(mUUID)
	if err != nil {
		return "", err
	}

	// This will break and die if an existing non-dir file exists
	existing := findExisting(dirs, mid)
	if existing != "" {
		return filepath.Join(conf.OutputDirectory, existing), nil
	}

	title, ok := m.Data.Attributes.Title["en"]

	// If there's no English title, pick any title at all. It doesn't matter.
	if !ok {
		for _, v := range m.Data.Attributes.Title {
			title = v
			break
		}
	}

	dirName := convertName(title) + " - " + mid
	dir := filepath.Join(conf.OutputDirectory, dirName)
	log.Debugln("Creating dir " + dir)
	return dir, os.Mkdir(dir, 0755)
}

func buildChapterArchiveName(c mangaChapter, cid string, groups map[string]string) string {
	out := ""
	if c.Data.Attributes.Volume != nil {
		out += "Vol. " + (string)(*c.Data.Attributes.Volume) + " "
	}

	out += "Ch. " + (string)(c.Data.Attributes.Chapter)

	if c.Data.Attributes.Title != "" {
		out += " " + c.Data.Attributes.Title
	}

	gns := []string{}
	for _, g := range groupIdsForChapter(c) {
		gn, ok := groups[g]
		if ok {
			gns = append(gns, gn)
		}
	}
	out += " [" + strings.Join(gns, ", ") + "]"

	return convertName(out) + " - " + cid + ".zip"
}

const chaptersURL = "https://api.mangadex.org/manga/%s/feed?limit=%d&offset=%d&translatedLanguage[]=%s&order[volume]=asc&order[chapter]=asc"

func getChapterPage(mid string, offset int) (chaptersResponse, error) {
	resp, err := client.Get(fmt.Sprintf(chaptersURL, mid, pageSize, offset, conf.Language))
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err)
		return chaptersResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorln("Manga "+mid, resp.Request.URL, errors.New(resp.Status), string(body))
		return chaptersResponse{}, err
	}

	var cr chaptersResponse
	err = json.Unmarshal(body, &cr)
	if err != nil {
		log.Errorln("Manga "+mid, resp.Request.URL, err, string(body))
		return chaptersResponse{}, err
	}

	return cr, nil
}

func getAllChapters(mid string) ([]mangaChapter, error) {
	total := 1
	offset := 0
	chapters := []mangaChapter{}

	for offset < total {
		<-time.After(delay)
		cr, err := getChapterPage(mid, offset)
		if err != nil {
			return nil, err
		}

		chapters = append(chapters, cr.Results...)
		total = cr.Total

		if len(cr.Results) != pageSize && offset+len(cr.Results) < total {
			log.Warningf("Manga %s: invalid chapter pagination. "+
				"Requested %d chapters at offset %d with %d total but got %d\n",
				mid, pageSize, offset, total, len(cr.Results))
		}

		offset += pageSize
	}

	return chapters, nil
}

func groupIdsForChapter(c mangaChapter) []string {
	groups := []string{}
	for _, r := range c.Relationships {
		if r.Type == "scanlation_group" {
			groups = append(groups, r.ID)
		}
	}

	return groups
}

func getAllGroups(chapters []mangaChapter) (map[string]string, error) {
	// Just handle up to 100 groups since it's rather unrealistic to plan for more.
	groups := make(map[string]string)

	for _, c := range chapters {
		for _, g := range groupIdsForChapter(c) {
			groups[g] = ""
		}
	}

	url := scanlationGroupsURL
	for gid := range groups {
		url += "&ids[]=" + gid
	}

	<-time.After(delay)

	resp, err := client.Get(url)
	if err != nil {
		log.Errorln(resp.Request.URL, err)
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorln(resp.Request.URL, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorln(resp.Request.URL, errors.New(resp.Status), string(body))
		return nil, err
	}

	var sg scanlationGroups
	err = json.Unmarshal(body, &sg)
	if err != nil {
		log.Errorln(resp.Request.URL, err, string(body))
		return nil, err
	}

	groups = make(map[string]string)
	for _, g := range sg.Results {
		groups[g.Data.ID] = g.Data.Attributes.Name
	}
	return groups, nil
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

	if m.Result != "ok" {
		log.Errorln("Manga "+mid, resp.Request.URL, errors.New(m.Result), string(body))
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

	chapters, err := getAllChapters(mid)
	if err != nil {
		log.Errorln("Manga "+mid, "Error fetching chapters", err)
		return
	}
	log.Debugf("Fetched %d chapters for %s\n", len(chapters), mangaDir)

	groups, err := getAllGroups(chapters)
	if err != nil {
		log.Errorln("Manga "+mid, "Error fetching scanlation groups", err)
		return
	}

	chs := make(map[string]bool)

	for _, c := range chapters {
		cid, err := convertUUID(c.Data.ID)
		if err != nil {
			log.Errorln("Manga "+mid, "Invalid chapter UUID", err)
			return
		}

		if chs[c.Data.ID] {
			// Turns out, mangadex doesn't have a default ordering for chapters.
			// I don't trust them to honour an explicit ordering either.
			log.Warningln("duplicate chapter ID " + c.Data.ID)
			continue
		}
		chs[c.Data.ID] = true

		if len(c.Data.Attributes.Data) == 0 {
			log.Debugln("Chapter "+cid, "Empty chapter", err)
			continue
		}

		if len(c.Data.Attributes.Data) == 1 &&
			strings.HasPrefix(c.Data.Attributes.Data[0], "https://") {
			log.Debugln("Chapter "+cid, "Chapter is externally hosted", err)
			continue
		}

		existing := findExisting(archives, cid)
		if err != nil {
			log.Errorln("Manga "+mid, "Error checking for existing archives", err)
			return
		}

		fileName := buildChapterArchiveName(c, cid, groups)
		filePath := filepath.Join(mangaDir, fileName)

		if existing != "" {
			if conf.RenameChapters && existing != fileName {
				// Don't check for existing files, just clobber them
				err = os.Rename(filepath.Join(mangaDir, existing), filePath)
				if err != nil {
					log.Errorln("Error renaming "+existing+" -> "+fileName, err)
				}
			}
			continue
		}

		select {
		case ch <- chapterJob{
			chapter:     c,
			archivePath: filePath,
		}:
		case <-closeChan:
			return
		}
	}
}
