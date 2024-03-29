package main

import (
	"confdecoder"
	"context"
	"os"
	"os/signal"
	"project/connector"
	"project/epolllistener"
	"project/logs/encode"
	"project/logs/logger"
	"strings"
	"syscall"

	"time"
)

type config struct {
	ListenLocal    string // куда стучатся сервисы, обслуживаемые этим конфигуратором (могут быть и не локальными)
	ListenExternal string // куда стучатся другие конфигураторы. можно ListenLocal = ListenExternal, если tcp
	Settings       string
}

const connectors_gopool_size int = 5
const settingsCheckTicktime time.Duration = time.Second * 5
const reconnectsCheckTicktime time.Duration = time.Second * 10
const reconnectsTargetBufSize int = 4

var thisConfOuterPort []byte

// TODO: рассылка обновлений по подпискам для своих (обслуживаемых локальным конфигуратором) удаленных сервисов будет работать будет через жопу (т.е. не будет работать)
// TODO: решить гемор с обменом со вторым конфигуратором данными о третьих конфигураторах, и как на это будет реагировать ридсеттингс
func main() {
	conf := &config{}
	err := confdecoder.DecodeFile("config.txt", conf)
	if err != nil {
		panic("read config file err: " + err.Error())
	}
	if (conf.ListenLocal == "" && conf.ListenExternal == "") || conf.Settings == "" {
		panic("some fields in config file are empty or not specified")
	}

	ctx, cancel := createContextWithInterruptSignal()

	flsh := logger.NewFlusher(encode.DebugLevel)
	l := flsh.NewLogsContainer("configurator")
	connector.SetupEpoll(func(e error) {
		l.Error("Epoll", e)
		cancel()
	})
	connector.SetupGopoolHandling(connectors_gopool_size, 1, connectors_gopool_size/2)

	epolllistener.SetupEpollErrorHandler(func(e error) {
		l.Error("Epoll", e)
		cancel()
	})
	initReconnection(ctx, reconnectsCheckTicktime, reconnectsTargetBufSize, 1)

	subs := newSubscriptions(ctx, l, 5, nil)

	servs := newServices(ctx, l, conf.Settings, settingsCheckTicktime, subs)
	subs.services = servs

	var local_ln, external_ln listenier
	var allow_remote_on_local_ln bool

	if conf.ListenExternal != "" {
		if conf.ListenExternal != conf.ListenLocal {
			if external_ln, err = newListener((conf.ListenExternal)[:strings.Index(conf.ListenExternal, ":")], (conf.ListenExternal)[strings.Index(conf.ListenExternal, ":")+1:], true, subs, servs, l); err != nil {
				l.Error("newListener remote", err)
				cancel()
			}
			thisConfOuterPort = []byte((conf.ListenExternal)[strings.LastIndex(conf.ListenExternal, ":")+1:])
		} else {
			thisConfOuterPort = []byte((conf.ListenLocal)[strings.LastIndex(conf.ListenLocal, ":")+1:])
			allow_remote_on_local_ln = true
		}
	}

	if local_ln, err = newListener((conf.ListenLocal)[:strings.Index(conf.ListenLocal, ":")], (conf.ListenLocal)[strings.Index(conf.ListenLocal, ":")+1:], allow_remote_on_local_ln, subs, servs, l); err != nil {
		panic("newListener local: " + err.Error())
	}

	<-ctx.Done()
	l.Debug("Context", "done, exiting")
	local_ln.close()
	if external_ln != nil {
		external_ln.close()
	}
	flsh.Close()
	flsh.DoneWithTimeout(time.Second * 5)
}

func createContextWithInterruptSignal() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		cancel()
	}()
	return ctx, cancel
}
