package main

import (
	"context"
	"thin-peak/httpservice"

	"github.com/big-larry/mgo"
	"github.com/thin-peak/logger"
)

type config struct {
	Configurator string
	Listen       string
	MgoDB        string
	MgoAddr      string
	MgoColl      string
	mgoSession   *mgo.Session
}

const thisServiceName httpservice.ServiceName = "quiz.setquizquestions"
const tokenDecoderServiceName httpservice.ServiceName = "identity.tokendecoder"
const authGetServiceName httpservice.ServiceName = "auth.get"

func (c *config) GetListenAddress() string {
	return c.Listen
}

func (c *config) GetConfiguratorAddress() string {
	return c.Configurator
}

func (c *config) CreateHandler(ctx context.Context, connectors map[httpservice.ServiceName]*httpservice.InnerService) (httpservice.HttpService, error) {
	mgoSession, err := mgo.Dial(c.MgoAddr)
	if err != nil {
		logger.Error("Mongo", err)
		return nil, err
	}
	c.mgoSession = mgoSession
	logger.Info("Mongo", "Connected!")
	mgoCollection := mgoSession.DB(c.MgoDB).C(c.MgoColl)
	return NewHandler(mgoCollection, connectors[authGetServiceName], connectors[tokenDecoderServiceName])
}

func (conf *config) Close() error {
	conf.mgoSession.Close()
	return nil
}

func main() {
	httpservice.InitNewService(thisServiceName, false, 5, &config{}, authGetServiceName, tokenDecoderServiceName)
}