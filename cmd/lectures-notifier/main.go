package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

type ebResp struct {
	Events     []event `json:"events"`
	Pagination struct {
		HasMoreItems bool `json:"has_more_items"`
	} `json:"pagination"`
}

type event struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Name struct {
		Text string `json:"text"`
	} `json:"name"`
	Start struct {
		Local string `json:"local"` // "YYYY-MM-DDTHH:MM:SS"
	} `json:"start"`
	Venue *struct {
		Address struct {
			Address1                string `json:"address_1"`
			Address2                string `json:"address_2"`
			City                    string `json:"city"`
			LocalizedAddressDisplay string `json:"localized_address_display"`
			PostalCode              string `json:"postal_code"`
		} `json:"address"`
	} `json:"venue"`
	TicketAvailability *struct {
		HasAvailableTickets *bool `json:"has_available_tickets"`
	} `json:"ticket_availability"`
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func mustEnv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		log.Fatalf("missing env var: %s", k)
	}
	return v
}

func isTicketsAvailable(e event) bool {
	if e.TicketAvailability == nil || e.TicketAvailability.HasAvailableTickets == nil {
		return false
	}
	return *e.TicketAvailability.HasAvailableTickets
}

func fetchAllLiveEvents(client *http.Client, orgID, token string) ([]event, error) {
	log.Println("starting to fetch live events from EventBrite")
	var all []event
	page := 1

	for {
		url := fmt.Sprintf(
			"https://www.eventbriteapi.com/v3/organizers/%s/events/?status=live&expand=venue,ticket_availability&page=%d",
			orgID, page,
		)
		log.Printf("fetching page %d from EventBrite", page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("error making request to EventBrite: %v", err)
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := fmt.Errorf("eventbrite status %d: %s", resp.StatusCode, string(body))
			log.Printf("error response from EventBrite: %v", err)
			return nil, err
		}

		var r ebResp
		if err := json.Unmarshal(body, &r); err != nil {
			log.Printf("error parsing EventBrite response: %v", err)
			return nil, err
		}

		log.Printf("fetched %d events from page %d", len(r.Events), page)
		all = append(all, r.Events...)
		if !r.Pagination.HasMoreItems {
			log.Println("no more pages available")
			break
		}
		page++
	}

	log.Printf("successfully fetched all %d live events", len(all))
	return all, nil
}

func publishNtfy(client *http.Client, topicURL, msg string) error {
	log.Println("publishing notification to ntfy")
	req, _ := http.NewRequest("POST", topicURL, bytes.NewBufferString(msg))
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("error posting to ntfy: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("ntfy status %d: %s", resp.StatusCode, string(b))
		log.Printf("error response from ntfy: %v", err)
		return err
	}
	return nil
}

func main() {
	log.Println("starting lectures-notifier")
	isLocal := os.Getenv("NTFY_TOPIC_URL") == ""
	if isLocal {
		log.Println("running in local mode")
	} else {
		log.Println("running in production mode")
		sleepDuration := time.Duration(rand.Intn(41)+10) * time.Second
		log.Printf("sleeping for %v before proceeding", sleepDuration)
		time.Sleep(sleepDuration)
	}

	log.Println("loading configuration from environment variables")
	orgID := mustEnv("EVENTBRITE_ORGANIZER_ID")
	token := mustEnv("EVENTBRITE_TOKEN")
	log.Printf("loaded organizer ID: %s", orgID)

	var ntfyTopicURL string
	if !isLocal {
		ntfyTopicURL = mustEnv("NTFY_TOPIC_URL")
		log.Println("loaded ntfy topic URL")
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	all, err := fetchAllLiveEvents(httpClient, orgID, token)
	if err != nil {
		log.Fatalf("failed to fetch events: %v", err)
	}

	var matches []event
	for _, e := range all {
		if !isTicketsAvailable(e) {
			continue
		}
		matches = append(matches, e)
	}

	log.Printf("found %d events with available tickets", len(matches))
	if len(matches) == 0 {
		log.Println("no events with available tickets, exiting")
		return
	}

	var b strings.Builder
	b.WriteString("Lectures On Tap: tickets available\n")
	for _, e := range matches {
		var timeStr string
		if len(e.Start.Local) >= len("2006-01-02T15:04:05") {
			t, err := time.Parse("2006-01-02T15:04:05", e.Start.Local)
			if err == nil {
				timeStr = t.Format("Mon, Jan 2 at 15:04")
			}
		}
		city := ""
		if e.Venue != nil {
			city = e.Venue.Address.City
		}
		fmt.Fprintf(&b, "- %s (%s) %s %s\n", e.Name.Text, timeStr, city, e.URL)
	}

	msg := b.String()
	if isLocal {
		log.Println("local mode: printing message to stdout")
		log.Print(msg)
	} else {
		if err := publishNtfy(httpClient, ntfyTopicURL, msg); err != nil {
			log.Fatalf("failed to publish notification: %v", err)
		}
		log.Println("notification published successfully")
	}
}
