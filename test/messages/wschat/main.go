package main

import (
	"context"
	"errors"
	"os"
	"sync"

	"project/test/repo/clickhouse"
	"project/test/types"
	"project/test/wsservice"
)

type userid string

const crt = `
CREATE TABLE IF NOT EXISTS messagestest (
	  UserId String
	, ChatId String
	, MessageType UInt8
	, Message String
	, Time DateTime
) ENGINE = MergeTree()
ORDER BY Time
`

type config struct {
	ClickhouseAddr  []string
	ClickhouseTable string
	FilesPath       string
}

type service struct {
	chconn *clickhouse.ClickhouseConnection

	path  string
	users map[userid]*userconns
	sync.RWMutex
}

const thisServiceName wsservice.ServiceName = "messages.get"

// один вебсокет чтобы править всеми // 12byte objectid

func (c *config) CreateHandlers(ctx context.Context, pubs_getter wsservice.Publishers_getter) (wsservice.Service, error) {
	if len(c.FilesPath) == 0 {
		return nil, errors.New("FilesPath not set")
	}
	if stat, err := os.Stat(c.FilesPath); err != nil {
		return nil, err
	} else if !stat.IsDir() {
		return nil, errors.New("FilePath is not a directory")
	}

	conn, err := clickhouse.Connect(ctx, c.ClickhouseAddr, c.ClickhouseTable, "default", "", "", 0, 0)
	if err != nil {
		return nil, err
	}
	if err := conn.Conn.Exec(ctx, crt); err != nil {
		panic(err)
	}
	return &service{
		chconn: conn,
		users:  make(map[userid]*userconns),
		path:   c.FilesPath,
	}, nil
}

// wsservice.Service interface implementation
func (s *service) CreateNewWsData(l types.Logger) wsservice.Handler {
	return &wsconn{
		l:    l,
		srvc: s,
	}
}

// wsservice.closer interface implementation
func (s *service) Close() error {
	return s.chconn.Close()
}

func (s *service) adduser(wsc *wsconn) {
	s.Lock()
	if uc, ok := s.users[wsc.userId]; ok {
		s.Unlock()
		uc.addconn(wsc)
	} else {
		s.users[wsc.userId] = createuserconns()
		s.Unlock()
	}
}

func main() {
	wsservice.InitNewServiceWithoutConfigurator(thisServiceName, &config{}, false, 1)
}