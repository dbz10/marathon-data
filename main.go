package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	parallelism       = 4
	baseUrl           = "https://sporoku.jp"
	listPath          = "/result/naha_20231203/list"
	raceId            = 3625
	startPage         = 1
	endPage           = 274
	timeSelector      = "body > div.container.main > div > div.col-md-9.col-md-push-3 > div.content > div > div > div:nth-child(3) > div > table > tbody > tr:nth-child(1) > td"
	netTimeSelector   = "body > div.container.main > div > div.col-md-9.col-md-push-3 > div.content > div > div > div:nth-child(3) > div > table > tbody > tr:nth-child(2) > td"
	placementSelector = "body > div.container.main > div > div.col-md-9.col-md-push-3 > div.content > div > div > div:nth-child(3) > div > table > tbody > tr:nth-child(3) > td"
)

func ExtractResultUrlsFromListUrl(url string, resultCh chan []string, semaphore chan struct{}) {
	semaphore <- struct{}{}
	defer func() {
		<-semaphore
	}()
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Encountered an error extracting links from page %s: %s\n", url, err)
	}

	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Encountered an error parsing the body of url %s: %s\n", url, err)
	}

	var results []string
	// Extract the table rows with the links to the details result page
	doc.Find(".result-table-td-btn").Each(func(i int, s *goquery.Selection) {
		sel := s.Find("a")
		if strings.TrimSpace(sel.Text()) == "詳細" {
			href, ok := sel.Attr("href")
			if ok {
				results = append(results, href)
			}
		}

	})
	resultCh <- results

}

type FinishResult struct {
	Name      string `json:"name"`
	Number    int    `json:"number"`
	ClockTime string `json:"clockTime"`
	NetTime   string `json:"netTime"`
	Placement int    `json:"placement"`
}

func ExtractDetailsFromIndividualResult(url string, resultCh chan FinishResult, semaphore chan struct{}) {
	semaphore <- struct{}{}
	defer func() {
		<-semaphore
	}()
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Encountered an error extracting finish details from %s: %s\n", url, err)
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Encountered an error trying to parse the body of %s: %s\n", url, err)
	}

	var result FinishResult

	// Extract name and number
	sel := doc.Find("div.infobox")
	sel.Each(func(i int, s *goquery.Selection) {
		texts := strings.Split(strings.TrimSpace(s.Text()), "\n")
		for i := range texts {
			texts[i] = strings.TrimSpace(texts[i])
		}
		// Extract number
		if texts[0] == "ナンバー" {
			n, err := strconv.Atoi(texts[1])
			if err != nil {
				log.Println("Couldn't convert %s to an integer: %s", texts[1], err)
			}
			result.Number = n
		}

		// Extract name
		if texts[0] == "氏名" {
			result.Name = texts[1]
		}
	})
	clockTime := doc.Find(timeSelector).Text()
	netTime := doc.Find(netTimeSelector).Text()
	placementStr := strings.TrimSpace(strings.ReplaceAll(doc.Find(placementSelector).Text(), "位", ""))
	placement, err := strconv.Atoi(placementStr)
	if err != nil {
		log.Printf("Encountered an error parsing duration %s, %s\n", placementStr, err)

	}

	result.ClockTime = clockTime
	result.NetTime = netTime
	result.Placement = placement
	resultCh <- result
}

func main() {
	log.Println("Gathering target pages")
	semaphore := make(chan struct{}, parallelism)
	ch := make(chan []string)
	for i := startPage; i <= endPage; i++ {
		urlParams := url.Values{}
		urlParams.Add("race_id", strconv.Itoa(raceId))
		urlParams.Add("page", strconv.Itoa(i))
		pageUrl := baseUrl + listPath + "?" + urlParams.Encode()
		go ExtractResultUrlsFromListUrl(pageUrl, ch, semaphore)
	}

	var targetPages []string
	// Read from the channel N times
	for i := startPage; i <= endPage; i++ {
		targetPages = append(targetPages, <-ch...)
	}
	close(ch)

	fmt.Println("Number of target pages:", len(targetPages))
	detailCh := make(chan FinishResult)
	for _, target := range targetPages {
		targetUrl := baseUrl + target
		go ExtractDetailsFromIndividualResult(targetUrl, detailCh, semaphore)
	}

	var finishResults []FinishResult
	for i := range targetPages {
		if i%100 == 0 {
			fmt.Println(i)
		}
		finishResults = append(finishResults, <-detailCh)
	}
	jsonified, err := json.MarshalIndent(finishResults, "", "  ")
	if err != nil {
		fmt.Printf("Couldn't serialize results to json %s", err)
	}
	f, err := os.Create("marathon.json")
	if err != nil {
		fmt.Println("Coudn't create a file to store the results")
	}
	defer f.Close()

	_, err = f.Write(jsonified)
	if err != nil {
		fmt.Printf("Couldn't write results: %s\n", err)
	}
}
