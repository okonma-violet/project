package main

import (
	"context"
	"os"
	"os/signal"

	"project/test/logscontainer"
	"project/test/logscontainer/flushers"

	"time"

	"github.com/BurntSushi/toml"
)

type config struct {
	Listen    string
	Settings  string
	Memcached string
	//Hosts    []string
}

func main() {
	conf := &config{}
	if _, err := toml.DecodeFile("config.toml", conf); err != nil {
		println("read toml err:", err)
		return
	}
	if conf.Listen == "" || conf.Settings == "" {
		println("some fields in conf.toml are empty or not specified")
	}

	ctx, cancel := CreateContextWithInterruptSignal()
	logsctx, logscancel := context.WithCancel(context.Background())

	l, err := logscontainer.NewLogsContainer(logsctx, flushers.NewConsoleFlusher("CNFG"), 1, time.Second, 1)
	if err != nil {
		println("Logs init err:", err)
		return
	}
	defer func() {
		cancel()
		logscancel()
		l.WaitAllFlushesDone()
	}()

	c, err := NewConfigurator(conf.Settings, conf.Memcached)
	if err != nil {
		l.Error("NewConfigurator", err)
		return
	}
	defer func() {

	}()
	if err = c.Serve(ctx, l, conf.Listen); err != nil {
		l.Error("Serve", err)
	}

}

func CreateContextWithInterruptSignal() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	go func() {
		<-stop
		cancel()
	}()
	return ctx, cancel
}
