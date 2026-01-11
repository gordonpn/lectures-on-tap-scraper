package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

func mustEnv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		panic("missing env var: " + k)
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
	var all []event
	page := 1

	for {
		url := fmt.Sprintf(
			"https://www.eventbriteapi.com/v3/organizers/%s/events/?status=live&expand=venue,ticket_availability&page=%d",
			orgID, page,
		)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("eventbrite status %d: %s", resp.StatusCode, string(body))
		}

		var r ebResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}

		all = append(all, r.Events...)
		if !r.Pagination.HasMoreItems {
			break
		}
		page++
	}

	return all, nil
}

func publishNtfy(client *http.Client, topicURL, msg string) error {
	req, _ := http.NewRequest("POST", topicURL, bytes.NewBufferString(msg))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func main() {
	isLocal := os.Getenv("NTFY_TOPIC_URL") == ""
	if !isLocal {
		sleepDuration := time.Duration(rand.Intn(41)+10) * time.Second
		fmt.Printf("Sleeping for %v\n", sleepDuration)
		time.Sleep(sleepDuration)
	}

	orgID := mustEnv("EVENTBRITE_ORGANIZER_ID")
	token := mustEnv("EVENTBRITE_TOKEN")

	var ntfyTopicURL string
	if !isLocal {
		ntfyTopicURL = mustEnv("NTFY_TOPIC_URL")
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	all, err := fetchAllLiveEvents(httpClient, orgID, token)
	if err != nil {
		panic(err)
	}

	var matches []event
	for _, e := range all {
		if !isTicketsAvailable(e) {
			continue
		}
		matches = append(matches, e)
	}

	if len(matches) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("Lectures On Tap: tickets available\n")
	for _, e := range matches {
		timeStr := ""
		if len(e.Start.Local) >= len("2006-01-02T15:04:05") {
			timeStr = e.Start.Local[len("2006-01-02T") : len("2006-01-02T")+5]
		}
		city := ""
		if e.Venue != nil {
			city = e.Venue.Address.City
		}
		b.WriteString(fmt.Sprintf("- %s (%s) %s %s\n", e.Name.Text, timeStr, city, e.URL))
	}

	msg := b.String()
	if isLocal {
		fmt.Print(msg)
	} else {
		if err := publishNtfy(httpClient, ntfyTopicURL, msg); err != nil {
			panic(err)
		}
	}
}
