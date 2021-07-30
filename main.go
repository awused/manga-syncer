package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/awused/awconf"
	log "github.com/sirupsen/logrus"
)

type config struct {
	Language           string
	Manga              []string
	OutputDirectory    string
	Threads            int
	TempDirectory      string
	RenameChapters     bool
	AllowQuestionMarks bool
	RenameManga        bool
}

var conf config

var closeChan = make(chan struct{})

var client *http.Client = &http.Client{
	Transport: &http.Transport{
		IdleConnTimeout: 30 * time.Second,
	},
}

var delay = 2 * time.Second

type stringable string

var sem chan struct{}

func (st *stringable) UnmarshalJSON(b []byte) error {
	if b[0] != '"' {
		var i int
		err := json.Unmarshal(b, &i)
		*st = (stringable)(strconv.Itoa(i))
		return err
	}
	return json.Unmarshal(b, (*string)(st))
}

// Finds an existing directory or file with the given manga/chapter ID. Not UUID.
// This handles cases where manga are renamed as long as the IDs are constant.
// O(n) for each search, but this is unlikely to ever add up to much.
func findExisting(files []os.FileInfo, id string) string {
	for _, f := range files {
		if strings.HasSuffix(strings.TrimSuffix(f.Name(), ".zip"), id) {
			return f.Name()
		}
	}
	return ""
}

// Only for print-unmatched, fine to be inefficient
func removeExisting(files []os.FileInfo, id string) []os.FileInfo {
	for i, f := range files {
		if strings.HasSuffix(strings.TrimSuffix(f.Name(), ".zip"), id) {
			return append(files[:i], files[i+1:]...)
		}
	}

	// Should never happen
	return files
}

// This is much more restrictive than what is truly needed for just safety.
var safeFilenameRegex = regexp.MustCompile(`[^’'",#!\(\)!\p{L}\p{N}-_+=\[\]. ]+`)
var safeQuestionMarkRegex = regexp.MustCompile(`[^?’'",#!\(\)!\p{L}\p{N}-_+=\[\]. ]+`)
var repeatedHyphens = regexp.MustCompile(`--+`)

func convertName(input string) string {
	output := safeFilenameRegex.ReplaceAllString(input, "-")
	output = repeatedHyphens.ReplaceAllString(output, "-")
	return strings.Trim(output, "- ")
}

func convertUUID(input string) (string, error) {
	u, err := uuid.Parse(input)
	if err != nil {
		log.Errorln("Invalid UUID string", input)
		return "", err
	}

	return strings.Trim(base64.URLEncoding.EncodeToString(u[:]), "="), nil
}

var printValid = flag.Bool("print-valid", false, "Print all valid chapter archives to stdout without downloading anything new.")
var printUmatched = flag.Bool("print-unmatched", false, "Print all chapter archives that exist in a manga directory but don't match a chapter on the remote host.")
var chapterFlag = flag.String("chapter", "", "Download only this chapter from the given manga.")

func main() {
	flag.Parse()

	if *printValid && *printUmatched {
		log.Fatalln("Can't specify print-valid and print-unmatched at the same time")
	}

	err := awconf.LoadConfig("manga-syncer", &conf)
	if err != nil {
		log.Fatal(err)
	}

	if conf.Threads <= 0 {
		log.Fatalln("Must specify at least one thread.")
	}

	if conf.AllowQuestionMarks {
		safeFilenameRegex = safeQuestionMarkRegex
	}

	// We can revisit this in the future but Mangadex in particular has a
	// low limit so additional threads are dangerous.
	// conf.Threads = 1

	wg := sync.WaitGroup{}
	sigs := make(chan os.Signal, 100)
	doneChan := make(chan struct{})
	chapterChan := make(chan chapterJob, conf.Threads*2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM)

	// for i := 0; i < conf.Threads; i++ {
	wg.Add(1)
	go chapterWorker(chapterChan, &wg)
	// }
	sem = make(chan struct{}, conf.Threads)

	manga := conf.Manga

	if flag.NArg() > 0 {
		mangaStrings := flag.Args()
		manga = []string{}
		for _, v := range mangaStrings {
			m := v
			manga = append(manga, m)
		}
	}

	if *chapterFlag != "" {
		mid, err := getMangaIDForChapter(*chapterFlag)
		if err != nil {
			log.Fatalln("Failed to get manga ID for chapter", *chapterFlag)
		}
		manga = []string{mid}
		delay = 0 // We will be making very few calls, so disable any delays
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(chapterChan)
		for _, m := range manga {
			select {
			case <-closeChan:
				return
			case <-time.After(delay):
			}
			syncManga(m, chapterChan)
		}
	}()

	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-sigs:
		log.Println("Cleaning up and exiting early.")
		close(closeChan)
	case <-doneChan:
	}

	wg.Wait()
}
