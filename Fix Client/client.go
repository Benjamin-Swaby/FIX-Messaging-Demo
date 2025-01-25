package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/quickfixgo/quickfix"
)

type Client struct {
	msg_chan chan quickfix.Message
}

func (e Client) OnCreate(sessionID quickfix.SessionID) {}

// OnLogon implemented as part of Application interface
func (e Client) OnLogon(sessionID quickfix.SessionID) {}

// OnLogout implemented as part of Application interface
func (e Client) OnLogout(sessionID quickfix.SessionID) {}

// FromAdmin implemented as part of Application interface
func (e Client) FromAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) (reject quickfix.MessageRejectError) {
	return nil
}

// ToAdmin implemented as part of Application interface
func (e Client) ToAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) {}

// ToApp implemented as part of Application interface
func (e Client) ToApp(msg *quickfix.Message, sessionID quickfix.SessionID) (err error) {
	fmt.Printf("Sending %s \n\r", msg.String())
	return
}

// FromApp implemented as part of Application interface. This is the callback for all Application level messages from the counter party.
func (e Client) FromApp(msg *quickfix.Message, sessionID quickfix.SessionID) (reject quickfix.MessageRejectError) {
	fmt.Printf("FromApp %s \n\r", msg.String())
	e.msg_chan <- *msg
	return
}

//go:embed config/Client.cfg
var configFS embed.FS

func main() {

	config, err := configFS.Open("config/Client.cfg")

	if err != nil {
		log.Fatalf("Error Opening config/Client.cfg %s \n\r", err)
	}

	if len(os.Args[1:]) > 1 {
		fmt.Printf("./client [TLScertFile] [TLSkeyFile]\n\r")
	}

	defer config.Close()

	appSettings, err := quickfix.ParseSettings(config)

	if err != nil {
		log.Fatalf("Error Parsing App Settings %s \n\r", err)
	}

	msg_chan := make(chan quickfix.Message)
	app := Client{msg_chan}

	fileLogFactory, err := quickfix.NewFileLogFactory(appSettings)

	if err != nil {
		log.Fatalf("Error Creating LogFactory %s \n\r", err)
	}

	messageStoreFactory := quickfix.NewMemoryStoreFactory()

	initiator, err := quickfix.NewInitiator(
		app,
		messageStoreFactory,
		appSettings,
		fileLogFactory)

	if err != nil {
		log.Fatalf("Error Creating Initiator %s \n\r", err)
	}

	err = initiator.Start()

	if err != nil {
		log.Fatalf("Error Starting Initiator %s \n\r", err)
	}

	wf := newWebsiteFrontend(msg_chan)

	if len(os.Args[1:]) >= 2 {
		wf.start_web_tls(":443", os.Args[1], os.Args[2])
	} else {
		wf.start_web(":8080")
	}

}
