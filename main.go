// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"flag"
	"github.com/nlopes/slack"
	"os"
	irc "github.com/thoj/go-ircevent"
	"github.com/golang/glog"
	"errors"
	"sync"
)

var ircServer = flag.String("irc-server", "irc.freenode.net:6667", "ircserver:port")
var ircChannel = flag.String("irc-channel", "", "IRC channel to bridge")
var slackChannel = flag.String("slack-channel", "", "Slack channel to bridge")
var ircNick = flag.String("irc-nick", "slackircbridge", "IRC Nick for bridge bot")
var ircUserName = flag.String("irc-user", "slackircbridge", "IRC username for bridge bot")

func connectIRC(c chan string) (*irc.Connection, error) { 
	con := irc.IRC(*ircNick, *ircUserName)
	err := con.Connect(*ircServer)
	if err != nil {
		return nil, err
	}

	con.AddCallback("001", func(e *irc.Event) {
		con.Join(*ircChannel)
	})

	con.AddCallback("PRIVMSG", func(e *irc.Event) {
		go func () {
			msg := fmt.Sprintf("<%s> %s", e.Nick, e.Message())
			c <- msg
		}()
	})

	go func() {
		con.Loop()
	}()

	return con, nil
}

func slackLoop(slackChan chan string, slackUsers map[string]string, rtm *slack.RTM) {
	for {
		select {
			case msg := <-rtm.IncomingEvents:
			switch ev := msg.Data.(type) {
				case *slack.HelloEvent:
					glog.V(2).Info("HelloEvent")

				case *slack.ConnectedEvent:
					glog.V(2).Info("Connected Event")

				case *slack.MessageEvent:
					glog.V(2).Info("Message Event")
					go func () {
						if ev.Text == "" {
							return
						}

						msg := fmt.Sprintf("<%s> %s", slackUsers[ev.User], ev.Text)
						slackChan <- msg
					}()

				case *slack.PresenceChangeEvent:
					glog.V(2).Info("PresenceChangeEvent")

				case *slack.LatencyReport:
					glog.V(2).Info("LatencyReport")

				case *slack.RTMError:
					glog.Error("RTMError")

				case *slack.InvalidAuthEvent:
					glog.Error("InvalidCredentials")

				case *slack.ConnectingEvent:
					glog.V(2).Info("ConnectingEvent attempt ", ev.Attempt)

				case *slack.ConnectionErrorEvent:
					glog.Error("ConnectionErrorEvent: ", ev.ErrorObj)

			}
		}
	}
}

func connectSlack(slackChan chan string) (string, *slack.RTM, error) {
	token := os.Getenv("SLACK_TOKEN")

	api := slack.New(token)

	channels, err := api.GetChannels(false)
	if err != nil {
		return "", nil, err
	}

	groups, err := api.GetGroups(false)
	if err != nil {
		return "", nil, err
	}

	var chanID string

	// see if the channel we want to bridge is in the private list
	for _, g := range groups {
		if g.Name == *slackChannel {
			chanID = g.ID
		} 
	}

	// if we haven't found the channel in the private list, look in public.
	if chanID == "" {
		for _, g := range channels {
			if g.Name == *slackChannel {
				chanID = g.ID
			} 
		}
	}

	if chanID == "" {
		return "", nil, errors.New("couldn't find slack channel id")
	}

	users, err := api.GetUsers()
	if err != nil {
		return "", nil, err
	}

	slackUsers := make(map[string]string)
	for _, u := range users {
		slackUsers[u.ID] = u.Name
	}
	
	rtm := api.NewRTM()

	go rtm.ManageConnection()

	go slackLoop(slackChan, slackUsers, rtm)

	return chanID, rtm, nil
}

func echoIRCToSlack(c chan string, rtm *slack.RTM, ID string) {
	for {
		msg := <- c
		rtm.SendMessage(rtm.NewOutgoingMessage(msg, ID))
	}
}

func echoSlackToIRC(c chan string, con *irc.Connection, channel string) {
	for {
		msg := <- c
		con.Privmsg(channel, msg)
	}
}

func usage() {
	fmt.Println("usage:")
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if *slackChannel == "" || *ircChannel == "" {
		os.Exit(1)
	}

	ircChan := make(chan string)
	slackChan := make(chan string)

	con, err := connectIRC(ircChan)
	if err != nil {
		glog.Fatal(err)
		os.Exit(1)
	}

	chanID, rtm, err := connectSlack(slackChan)
	if err != nil {
		glog.Fatal(err)
		os.Exit(1)
	}

	go echoIRCToSlack(ircChan, rtm, chanID)
	go echoSlackToIRC(slackChan, con, *ircChannel)

	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}
