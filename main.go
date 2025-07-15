package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	Mail     string `yaml:"mail"`
	Password string `yaml:"password"`
	BotID    string `yaml:"bot_id"`
	ChatID   string `yaml:"chat_id"`
	ImapAddr string `yaml:"imap_addr"`
}

var config Config

var (
	lastMessageTime time.Time
	filename        = "last.txt"
)

func init() {
	rawConfig, err := os.ReadFile("config.yml")
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		os.WriteFile(filename, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0777)
	}
	i, _ := strconv.ParseInt(strings.Trim(string(data), "\r\n"), 10, 64)
	if i > 0 {
		lastMessageTime = time.Unix(i, 0)
	}
}

type TgMsg struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

func sendToTg(msg []byte) {
	r := bytes.NewReader(msg)
	http.Post(fmt.Sprintf("https://api.telegram.org/%s/sendMessage", config.BotID), "application/json", r)
}
func fetchAndSend(limit uint32) {
	log.Println("connecting to", config.ImapAddr)
	c, err := client.DialTLS(config.ImapAddr, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Don't forget to logout
	defer c.Logout()

	// Login
	if err := c.Login(config.Mail, config.Password); err != nil {
		log.Fatal(err)
	}
	done := make(chan error, 1)
	log.Println("Logged in")

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Fatal(err)
	}

	from := uint32(1)
	to := mbox.Messages
	if mbox.Messages > limit {
		// We're using unsigned integers here, only substract if the result is > 0
		from = mbox.Messages - limit
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messages := make(chan *imap.Message, 10)
	done = make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	for msg := range messages {
		sender := msg.Envelope.Sender
		from := ""
		for _, s := range sender {
			from += s.Address()
			if len(s.PersonalName) > 0 {
				from += ` (` + s.PersonalName + `)`
			}
		}
		if msg.Envelope.Date.After(lastMessageTime) {
			log.Println("Found new message")
			lastMessageTime = msg.Envelope.Date
			os.WriteFile(filename, []byte(fmt.Sprintf("%d", lastMessageTime.Unix())), 0777)
			msg := TgMsg{
				ChatID: config.ChatID,
				Text:   fmt.Sprintf("* %s: %s", from, msg.Envelope.Subject),
			}
			encoded, _ := json.Marshal(msg)
			sendToTg(encoded)
		}
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}

	log.Println("Done!")
}

func main() {
	fetchAndSend(1)
	for {
		fetchAndSend(10)
		time.Sleep(time.Minute)
	}
}
