package main

import (
	"strings"
	"thin-peak/logs/logger"

	"github.com/big-larry/mgo"
	"github.com/big-larry/mgo/bson"
	"github.com/big-larry/suckhttp"
)

type RenameFolder struct {
	mgoSession *mgo.Session
	mgoColl    *mgo.Collection
}

func NewRenameFolder(mgodb string, mgoAddr string, mgoColl string) (*RenameFolder, error) {

	mgoSession, err := mgo.Dial(mgoAddr)
	if err != nil {
		logger.Error("Mongo conn", err)
		return nil, err
	}
	logger.Info("Mongo", "Connected!")

	mgoCollection := mgoSession.DB(mgodb).C(mgoColl)

	return &RenameFolder{mgoSession: mgoSession, mgoColl: mgoCollection}, nil

}

func (conf *RenameFolder) Close() error {
	conf.mgoSession.Close()
	return nil
}

func (conf *RenameFolder) Handle(r *suckhttp.Request, l *logger.Logger) (*suckhttp.Response, error) {

	if r.GetMethod() != suckhttp.HttpMethod("PATCH") {
		return suckhttp.NewResponse(400, "Bad request"), nil
	}

	fid := r.Uri.Path
	fnewname := strings.TrimSpace(r.Uri.Query().Get("fnewname"))
	if fid == "" || fnewname == "" {
		return suckhttp.NewResponse(400, "Bad request"), nil
	}

	// TODO: AUTH

	query := &bson.M{"_id": fid, "deleted": bson.M{"$exists": false}}

	change := mgo.Change{
		Update:    &bson.M{"$set": bson.M{"name": fnewname}},
		Upsert:    false,
		ReturnNew: true,
		Remove:    false,
	}

	if _, err := conf.mgoColl.Find(query).Apply(change, nil); err != nil {
		if err == mgo.ErrNotFound {
			return suckhttp.NewResponse(403, "Forbidden"), nil
		}
		return nil, err
	}

	return suckhttp.NewResponse(200, "OK"), nil
}
