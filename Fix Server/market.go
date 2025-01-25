package main

import (
	"fmt"

	"github.com/quickfixgo/enum"
	"github.com/shopspring/decimal"

	fix50nos "github.com/quickfixgo/fix50/newordersingle"
	fix50cxl "github.com/quickfixgo/fix50/ordercancelrequest"
	fix50osr "github.com/quickfixgo/fix50/orderstatusrequest"
)

type market struct {
	nsoChannel       chan *fix50nos.NewOrderSingle
	cancelChannel    chan *fix50cxl.OrderCancelRequest
	queryChannel     chan *fix50osr.OrderStatusRequest
	respChannel      chan OrderExecutionStatus
	queryRespChannel chan *SingleOrder

	orders map[string][]SingleOrder
}

type Side int

const (
	BUY  = 0
	SELL = 1
)

type OrderType int

const (
	LIMIT  = 0
	MARKET = 1
)

type OrderExecutionStatus int

const (
	NSO_PLACED = iota
	NSO_FAILED
	NSO_FAILED_ORDER_EXISTS
	CANCEL_CANCELLED
	CANCEL_FAILED
	CANCEL_NO_SUCH_ORDER
	QUERY_NO_SUCH_ORDER
	QUERY_ORDER_FOUND
)

type SingleOrder struct {
	id        string
	user      string
	symbol    string
	volume    decimal.Decimal
	price     decimal.Decimal
	side      Side
	orderType OrderType
}

func (m market) listenNSO() {
	for {
		fmt.Println("NSO Channel Idle")

		msg := <-m.nsoChannel

		so := SingleOrder{}
		so.user, _ = msg.GetSenderSubID()
		so.symbol, _ = msg.GetSymbol()
		so.volume, _ = msg.GetOrderQty()
		so.price, _ = msg.GetPrice()
		so.id, _ = msg.GetClOrdID()

		side, _ := msg.GetSide()
		if side == enum.Side_BUY {
			so.side = BUY
		} else {
			so.side = SELL
		}

		ot, _ := msg.GetOrdType()
		if ot == enum.OrdType_MARKET {
			so.orderType = MARKET
		} else {
			so.orderType = LIMIT
		}

		unique := true
		for _, order := range m.orders[so.symbol] {
			if order.id == so.id && so.user == order.user {
				unique = false
				m.respChannel <- NSO_FAILED_ORDER_EXISTS
				fmt.Printf("New Single Order (%s) From %s NOT Placed\n\r", so.id, so.user)
				break
			}
		}

		if unique {
			fmt.Printf("New Single Order (%s) From %s Placed\n\r", so.id, so.user)
			m.orders[so.symbol] = append(m.orders[so.symbol], so)
			m.respChannel <- NSO_PLACED
		}

	}
}

func (m market) listenCancel() {
	for {
		fmt.Println("Cancel Channel Idle!")

		msg := <-m.cancelChannel

		ordId, _ := msg.GetOrigClOrdID()
		user, _ := msg.GetSenderSubID()
		ticker, _ := msg.GetSymbol()

		found := false
		// search through orders and cancel if possible
		for i, order := range m.orders[ticker] {
			if ordId == order.id && user == order.user {
				m.orders[ticker] = append(m.orders[ticker][:i], m.orders[ticker][i+1:]...)
				fmt.Printf("Canceled Order (%s) by %s \n\r", ordId, user)
				m.respChannel <- CANCEL_CANCELLED
				found = true
				break
			}
		}

		if !found {
			m.respChannel <- CANCEL_NO_SUCH_ORDER
		}

	}
}

func (m market) listenQuery() {
	for {
		fmt.Println("Query Channel Idle!")
		msg := <-m.queryChannel

		ordId, _ := msg.GetOrdStatusReqID()
		user, _ := msg.GetSenderSubID()
		ticker, _ := msg.GetSymbol()

		found := false
		// search through orders and cancel if possible
		for _, order := range m.orders[ticker] {
			if ordId == order.id && user == order.user {
				fmt.Printf("Queried Order (%s) by %s \n\r", ordId, user)
				m.respChannel <- QUERY_ORDER_FOUND
				m.queryRespChannel <- &order
				found = true
				break
			}
		}

		if !found {
			m.respChannel <- CANCEL_NO_SUCH_ORDER
		}

	}
}

func (m market) startMarket() {
	m.orders = make(map[string][]SingleOrder)
	go m.listenNSO()
	go m.listenQuery()
	go m.listenCancel()
}
