package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/awused/awconf"
	log "github.com/sirupsen/logrus"
)

type config struct {
	Language        string
	Manga           []int
	OutputDirectory string
	Threads         int
	TempDirectory   string
}

var conf config

var closeChan = make(chan struct{})

var client *http.Client = &http.Client{
	Transport: &http.Transport{
		IdleConnTimeout: 30 * time.Second,
	},
}

// Finds an existing directory or file with the given manga/chapter ID.
// This handles cases where manga are renamed as long as the IDs are constant.
// O(n) for each search, but this is unlikely to ever add up to much.
func findExisting(files []os.FileInfo, id string) string {
	for _, f := range files {
		if strings.HasSuffix(strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())), id) {
			return f.Name()
		}
	}
	return ""
}

var safeFilenameRegex = regexp.MustCompile(`[^\p{L}\p{N}-_+=[\]. ]+`)
var repeatedHyphens = regexp.MustCompile(`--+`)

func convertName(input string) string {
	output := safeFilenameRegex.ReplaceAllString(input, "-")
	output = repeatedHyphens.ReplaceAllString(output, "-")
	return strings.Trim(output, "- ")
}

func main() {
	flag.Parse()

	err := awconf.LoadConfig("manga-syncer", &conf)
	if err != nil {
		log.Fatal(err)
	}

	// We can revisit this in the future but Mangadex in particular has a
	// low limit so additional threads are dangerous.
	conf.Threads = 1

	wg := sync.WaitGroup{}
	sigs := make(chan os.Signal, 100)
	doneChan := make(chan struct{})
	chapterChan := make(chan chapterJob, conf.Threads*2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM)

	for i := 0; i < conf.Threads; i++ {
		wg.Add(1)
		go chapterWorker(chapterChan, &wg)
	}

	manga := conf.Manga

	if len(os.Args) > 1 {
		mangaStrings := os.Args[1:]
		manga = []int{}
		for _, v := range mangaStrings {
			m, err := strconv.Atoi(v)
			if err != nil {
				log.Fatal(err)
			}
			manga = append(manga, m)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, m := range manga {
			select {
			case <-closeChan:
				return
			case <-time.After(5 * time.Second):
			}
			syncManga(strconv.Itoa(m), chapterChan)
		}

		close(chapterChan)
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
