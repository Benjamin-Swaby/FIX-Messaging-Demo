package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
	"github.com/shopspring/decimal"

	fix50er "github.com/quickfixgo/fix50/executionreport"
	fix50nos "github.com/quickfixgo/fix50/newordersingle"
	fix50cxl "github.com/quickfixgo/fix50/ordercancelrequest"
	fix50osr "github.com/quickfixgo/fix50/orderstatusrequest"
)

type Server struct {
	nsoChannel       chan *fix50nos.NewOrderSingle
	cancelChannel    chan *fix50cxl.OrderCancelRequest
	queryChannel     chan *fix50osr.OrderStatusRequest
	marketChannel    chan OrderExecutionStatus
	queryRespChannel chan *SingleOrder
	orderID          int
	execID           int
	*quickfix.MessageRouter
}

func newServer() *Server {
	e := &Server{MessageRouter: quickfix.NewMessageRouter()}

	e.nsoChannel = make(chan *fix50nos.NewOrderSingle)
	e.cancelChannel = make(chan *fix50cxl.OrderCancelRequest)
	e.queryChannel = make(chan *fix50osr.OrderStatusRequest)
	e.marketChannel = make(chan OrderExecutionStatus)
	e.queryRespChannel = make(chan *SingleOrder)

	e.AddRoute(fix50nos.Route(e.OnFIX50NewOrderSingle))
	e.AddRoute(fix50cxl.Route(e.onFIX50OrderCancelRequest))
	e.AddRoute(fix50osr.Route(e.onFix50OrderStatusRequest))

	return e
}

func (e *Server) genOrderID() field.OrderIDField {
	e.orderID++
	return field.NewOrderID(strconv.Itoa(e.orderID))
}

func (e *Server) genExecID() field.ExecIDField {
	e.execID++
	return field.NewExecID(strconv.Itoa(e.execID))
}

// quickfix.Application interface
func (e Server) OnCreate(sessionID quickfix.SessionID)                           {}
func (e Server) OnLogon(sessionID quickfix.SessionID)                            {}
func (e Server) OnLogout(sessionID quickfix.SessionID)                           {}
func (e Server) ToAdmin(msg *quickfix.Message, sessionID quickfix.SessionID)     {}
func (e Server) ToApp(msg *quickfix.Message, sessionID quickfix.SessionID) error { return nil }
func (e Server) FromAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	return nil
}

// Use Message Cracker on Incoming Application Messages
func (e *Server) FromApp(msg *quickfix.Message, sessionID quickfix.SessionID) (reject quickfix.MessageRejectError) {
	return e.Route(msg, sessionID)
}

func (e *Server) onFix50OrderStatusRequest(msg fix50osr.OrderStatusRequest, sessionID quickfix.SessionID) quickfix.MessageRejectError {

	if !msg.HasOrdStatusReqID() {
		return quickfix.ValueIsIncorrect(tag.OrdStatusReqID)
	}

	if !msg.HasSenderSubID() {
		return quickfix.ValueIsIncorrect(tag.SenderSubID)
	}

	if !msg.HasSymbol() {
		return quickfix.ValueIsIncorrect(tag.Symbol)
	}

	side, _ := msg.GetSide()
	symbol, _ := msg.GetSymbol()
	subId, _ := msg.GetSenderSubID()

	execReport := fix50er.New(
		e.genOrderID(),
		e.genExecID(),
		field.NewExecType(enum.ExecType_ORDER_STATUS),
		field.NewOrdStatus(enum.OrdStatus_PARTIALLY_FILLED),
		field.NewSide(side),
		field.NewLeavesQty(decimal.Zero, 2),
		field.NewCumQty(decimal.Zero, 2),
	)

	execReport.SetTargetSubID(subId)
	execReport.SetSymbol(symbol)

	e.queryChannel <- &msg

	resp := <-e.marketChannel

	switch resp {
	case QUERY_NO_SUCH_ORDER:
		return quickfix.ValueIsIncorrect(tag.OrdStatusReqID)
	case QUERY_ORDER_FOUND:

		order := <-e.queryRespChannel

		execReport.SetPrice(order.price, 0)
		execReport.SetOrderQty(order.volume, 0)
		execReport.SetText("Order Is Considered Half Filled Filled")
		execReport.SetLeavesQty(order.volume.Div(decimal.NewFromInt(2)), 0)
		execReport.SetCumQty(order.volume.Div(decimal.NewFromInt(2)), 0)

	}

	quickfix.SendToTarget(execReport, sessionID)

	return nil

}

func (e *Server) onFIX50OrderCancelRequest(msg fix50cxl.OrderCancelRequest, sessionID quickfix.SessionID) quickfix.MessageRejectError {

	if !msg.HasClOrdID() {
		return quickfix.ValueIsIncorrect(tag.ClOrdID)
	}

	if !msg.HasOrigClOrdID() {
		return quickfix.ValueIsIncorrect(tag.OrigClOrdID)
	}

	if !msg.HasSenderSubID() {
		return quickfix.ValueIsIncorrect(tag.SenderSubID)
	}

	if !msg.HasSymbol() {
		return quickfix.ValueIsIncorrect(tag.Symbol)
	}

	e.cancelChannel <- &msg

	mkrt := <-e.marketChannel

	side, _ := msg.GetSide()
	symbol, _ := msg.GetSymbol()
	subId, _ := msg.GetSenderSubID()

	execReport := fix50er.New(
		e.genOrderID(),
		e.genExecID(),
		field.NewExecType(enum.ExecType_CANCELED),
		field.NewOrdStatus(enum.OrdStatus_CANCELED),
		field.NewSide(side),
		field.NewLeavesQty(decimal.Zero, 2),
		field.NewCumQty(decimal.Zero, 2),
	)

	execReport.SetTargetSubID(subId)
	execReport.SetSymbol(symbol)

	switch mkrt {
	case CANCEL_CANCELLED:
		execReport.SetText("Order has Been Cancelled")

	case CANCEL_FAILED:
		execReport.SetOrdStatus(enum.OrdStatus_REJECTED)
		execReport.SetOrdRejReason(enum.OrdRejReason_BROKER)
		execReport.SetText("Failed To Cancel Order - Unkown Reason.")

	case CANCEL_NO_SUCH_ORDER:
		execReport.SetOrdStatus(enum.OrdStatus_REJECTED)
		execReport.SetOrdRejReason(enum.OrdRejReason_BROKER)
		execReport.SetText("Failed To Cancel Order - No Such Order")

	}

	quickfix.SendToTarget(execReport, sessionID)

	return nil
}

func (e *Server) OnFIX50NewOrderSingle(msg fix50nos.NewOrderSingle, sessionID quickfix.SessionID) (err quickfix.MessageRejectError) {

	//ordType, err := msg.GetOrdType()
	if err != nil {
		return err
	}

	symbol, err := msg.GetSymbol()
	if err != nil {
		return
	}

	side, err := msg.GetSide()
	if err != nil {
		return
	}

	orderQty, err := msg.GetOrderQty()
	if err != nil {
		return
	}

	price, err := msg.GetPrice()
	if err != nil {
		return
	}

	clOrdID, err := msg.GetClOrdID()
	if err != nil {
		return
	}

	execReport := fix50er.New(
		e.genOrderID(),
		e.genExecID(),
		field.NewExecType(enum.ExecType_FILL),
		field.NewOrdStatus(enum.OrdStatus_FILLED),
		field.NewSide(side),
		field.NewLeavesQty(decimal.Zero, 2),
		field.NewCumQty(orderQty, 2),
	)

	execReport.SetClOrdID(clOrdID)
	execReport.SetSymbol(symbol)
	execReport.SetOrderQty(orderQty, 2)
	execReport.SetLastQty(orderQty, 2)
	execReport.SetLastPx(price, 2)
	execReport.SetAvgPx(price, 2)

	e.nsoChannel <- &msg

	mkrt := <-e.marketChannel
	if mkrt == NSO_FAILED_ORDER_EXISTS {
		execReport.SetOrdStatus(enum.OrdStatus_REJECTED)
		execReport.SetOrdRejReason(enum.OrdRejReason_DUPLICATE_ORDER)
		execReport.SetText("Duplicate Order Placed")
	}

	sendErr := quickfix.SendToTarget(execReport, sessionID)

	if sendErr != nil {
		fmt.Println("Failed To Send", sendErr)
	}

	return
}

//go:embed config/Server.cfg
var cfgFS embed.FS

func main() {

	//cfg, err := os.Open("./Server.cfg")
	cfg, err := cfgFS.Open("config/Server.cfg")

	if err != nil {
		log.Fatalf("Failed to Open config/Server.cfg %s \n\r", err)
	}

	defer cfg.Close()

	appSettings, err := quickfix.ParseSettings(cfg)

	if err != nil {
		log.Fatalf("Failed to Parse Config %s \n\r", err)
	}

	app := newServer()

	market := market{
		nsoChannel:       app.nsoChannel,
		cancelChannel:    app.cancelChannel,
		queryChannel:     app.queryChannel,
		respChannel:      app.marketChannel,
		queryRespChannel: app.queryRespChannel,
	}

	market.startMarket()

	logFactory, err := quickfix.NewFileLogFactory(appSettings)

	if err != nil {
		fmt.Printf("Failed to Create File Log Factory! %s \n", err)
		fmt.Printf("Using Null Log Factory Instead (No Logs)\n")
		logFactory = quickfix.NewNullLogFactory()

	}

	acceptor, err := quickfix.NewAcceptor(app,
		quickfix.NewMemoryStoreFactory(),
		appSettings,
		logFactory)

	err = acceptor.Start()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt

	fmt.Println("Stopping Acceptor Service")
	acceptor.Stop()
	fmt.Println("Stopped")

}
