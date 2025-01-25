package main

import (
	"embed"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/shopspring/decimal"

	fix50cxl "github.com/quickfixgo/fix50/ordercancelrequest"
	fix50osr "github.com/quickfixgo/fix50/orderstatusrequest"
)

type OrderType int

const (
	Limit  = 0
	Market = 1
)

type Side int

const (
	Buy  = 0
	Sell = 1
)

type OrderDetails struct {
	ticker      string
	price       decimal.Decimal
	volume      decimal.Decimal
	orderType   enum.OrdType
	side        enum.Side
	oid         string
	senderSubId string
}

func formFixMessage(o OrderDetails) *quickfix.Message {

	// Header:
	msg := quickfix.NewMessage()
	msg.Header.Set(field.NewBeginString(quickfix.BeginStringFIXT11))
	msg.Header.Set(field.NewMsgType(enum.MsgType_ORDER_SINGLE))
	msg.Header.Set(field.NewSenderCompID("Client")) // make sure these are correct!
	msg.Header.Set(field.NewTargetCompID("Exchange"))
	msg.Header.Set(field.NewSenderSubID(o.senderSubId))
	msg.Header.Set(field.NewSendingTime(time.Now()))

	msg.Body.Set(field.NewClOrdID(o.oid)) // Order ID - Randomly generate this
	msg.Body.Set(field.NewHandlInst(enum.HandlInst_AUTOMATED_EXECUTION_ORDER_PRIVATE_NO_BROKER_INTERVENTION))
	msg.Body.Set(field.NewSymbol(o.ticker))
	msg.Body.Set(field.NewSide(o.side))
	msg.Body.Set(field.NewTransactTime(time.Now()))
	msg.Body.Set(field.NewOrdType(o.orderType))
	msg.Body.Set(field.NewOrderQty(o.volume, 0))
	msg.Body.Set(field.NewPrice(o.price, 0))

	return msg

}

func prettyPrintStr(msg quickfix.Message) string {

	msg_s := strings.ReplaceAll(msg.String(), string(uint64(1)), "\n")

	return msg_s

}

type website_frontend struct {
	//templates map[string]*template.Template
	templates *template.Template
	msgs      chan quickfix.Message
	users     map[string]string
}

func (wf website_frontend) root(w http.ResponseWriter, r *http.Request) {

	wf.templates.ExecuteTemplate(w, "index.html", nil)

}

func (wf website_frontend) cancelOrder(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		wf.templates.ExecuteTemplate(w, "cancelOrder.html", nil)
		return
	}

	originID := r.FormValue("originalOrderID")
	clordID := r.FormValue("cloid")
	subId := r.FormValue("subID")
	ticker := r.FormValue("Ticker")
	side := enum.Side_SELL

	if r.FormValue("Side") == "BUY" {
		side = enum.Side_BUY
	}

	cancel := fix50cxl.New(field.NewOrigClOrdID(originID), field.NewClOrdID(clordID), field.NewSide(side), field.NewTransactTime(time.Now()))
	cancel.Body.Set(field.NewSymbol(ticker))
	cancel.Header.Set(field.NewSenderSubID(subId))
	cancel.Header.Set(field.NewSenderCompID("Client")) // make sure these are correct!
	cancel.Header.Set(field.NewTargetCompID("Exchange"))

	quickfix.Send(cancel)
	resp := <-wf.msgs

	wf.templates.ExecuteTemplate(w, "cancelOrder.html",
		struct {
			Success  bool
			Message  string
			Response string
		}{true, prettyPrintStr(*cancel.Message), prettyPrintStr(resp)})

}

func (wf website_frontend) placeOrder(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		wf.templates.ExecuteTemplate(w, "placeOrder.html", nil)
		return
	}

	var err error

	orderDetails := OrderDetails{}
	orderDetails.ticker = r.FormValue("Ticker")

	orderDetails.volume, err = decimal.NewFromString(r.FormValue("Volume"))
	orderDetails.price, err = decimal.NewFromString(r.FormValue("Price"))
	orderDetails.senderSubId = r.FormValue("subID")

	orderDetails.oid = r.FormValue("oid")

	if err != nil {
		// make them re-enter the form
		wf.templates.ExecuteTemplate(w, "PlaceOrder", nil)
		return
	}

	if r.FormValue("OrderType") == "Market" {
		orderDetails.orderType = enum.OrdType_MARKET
	} else {
		orderDetails.orderType = enum.OrdType_LIMIT
	}

	if r.FormValue("Side") == "BUY" {
		orderDetails.side = enum.Side_BUY
	} else {
		orderDetails.side = enum.Side_SELL
	}

	msg := formFixMessage(orderDetails)

	err = quickfix.Send(msg)
	if err != nil {
		log.Fatalf("Failed To Send Message %s \n\r", err)
	}

	resp := <-wf.msgs

	wf.templates.ExecuteTemplate(w, "placeOrder.html",
		struct {
			Success  bool
			Message  string
			Response string
		}{true, prettyPrintStr(*msg), prettyPrintStr(resp)})

}

func (wf website_frontend) getSlides(w http.ResponseWriter, r *http.Request) {

	f, err := templateFS.Open("templates/slides.pdf")

	if err != nil {
		http.Error(w, "File not found.", http.StatusNotFound)
		return
	}

	defer f.Close()

	_, err = io.Copy(w, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

}

func (wf website_frontend) orderStatus(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		wf.templates.ExecuteTemplate(w, "orderStatus.html", nil)
		return
	}

	originalOrderID := r.FormValue("originalOrderID")
	clordID := r.FormValue("cloid")
	subId := r.FormValue("subID")
	ticker := r.FormValue("Ticker")
	side := enum.Side_SELL

	if r.FormValue("Side") == "BUY" {
		side = enum.Side_BUY
	}

	status := fix50osr.New(
		field.NewClOrdID(clordID),
		field.NewSide(side))

	status.SetOrdStatusReqID(originalOrderID)
	status.SetSenderSubID(subId)
	status.SetSymbol(ticker)

	status.Header.Set(field.NewSenderSubID(subId))
	status.Header.Set(field.NewSenderCompID("Client")) // make sure these are correct!
	status.Header.Set(field.NewTargetCompID("Exchange"))

	err := quickfix.Send(status)
	if err != nil {
		log.Fatalf("Failed to Send Message %s \n\r", err)
	}

	resp := <-wf.msgs

	wf.templates.ExecuteTemplate(w, "orderStatus.html",
		struct {
			Success  bool
			Message  string
			Response string
		}{true, prettyPrintStr(*status.Message), prettyPrintStr(resp)})

}

func newWebsiteFrontend(msg_chan chan quickfix.Message) website_frontend {
	wf := website_frontend{}
	wf.msgs = msg_chan

	wf.templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

	http.HandleFunc("/", wf.root)
	http.HandleFunc("/place", wf.placeOrder)
	http.HandleFunc("/cancel", wf.cancelOrder)
	http.HandleFunc("/status", wf.orderStatus)
	http.HandleFunc("/slides", wf.getSlides)

	return wf

}

//go:embed templates/*
var templateFS embed.FS

func (wf website_frontend) start_web_tls(addr, tlsCertFile, tlsKeyFile string) {

	// Whoo HTTPS
	err := http.ListenAndServeTLS(addr, tlsCertFile, tlsKeyFile, nil)

	if err != nil {
		log.Fatalf("Failed to Start Web Interface With HTTPS: %s\n\r", err)
	}

}

func (wf website_frontend) start_web(addr string) {

	// Whoo HTTPS
	err := http.ListenAndServe(addr, nil)

	if err != nil {
		log.Fatalf("Failed to Start Web Interface: %s\n\r", err)
	}

}
