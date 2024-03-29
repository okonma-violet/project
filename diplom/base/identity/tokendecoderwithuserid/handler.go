package main

import (
	"encoding/json"
	"errors"
	"project/base/identity/repo"

	"strings"
	"thin-peak/logs/logger"

	"github.com/big-larry/suckhttp"
	"github.com/dgrijalva/jwt-go"
	"github.com/tarantool/go-tarantool"
)

type Handler struct {
	trntlConn  *tarantool.Connection
	trntlTable string
	jwtKey     []byte
	cookieName string
}

func NewHandler(trntlAddr, trntlTable, jwtKey, cookieName string) (*Handler, error) {
	trntlConnection, err := repo.ConnectToTarantool(trntlAddr)
	if err != nil {
		return nil, err
	}

	return &Handler{trntlConn: trntlConnection, trntlTable: trntlTable, jwtKey: []byte(jwtKey), cookieName: cookieName}, nil
}

func (c *Handler) Close() error {
	return c.trntlConn.Close()
}

func (conf *Handler) Handle(r *suckhttp.Request, l *logger.Logger) (*suckhttp.Response, error) {
	if r.GetMethod() != suckhttp.GET {
		l.Debug("Request", "not GET")
		return suckhttp.NewResponse(400, "Bad request"), nil
	}

	tokenString := strings.Trim(r.Uri.Path, "/")
	if tokenString == "" {
		if tokenString, _ = r.GetCookie(conf.cookieName); tokenString == "" {
			l.Debug("Request", "not tokenString in both path and cookies header")
			return suckhttp.NewResponse(400, "Bad request"), nil
		}
	}

	cookieData := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, cookieData, func(token *jwt.Token) (interface{}, error) {
		return conf.jwtKey, nil
	})
	if err != nil {
		l.Error("Parsing token string", err)
		return suckhttp.NewResponse(400, "Bad request"), nil
	}

	if userlogin, ok := cookieData["login"]; ok {
		//trntl req
		var trntlRes []repo.TarantoolAuthTuple
		err = conf.trntlConn.SelectTyped(conf.trntlTable, "primary", 0, 1, tarantool.IterEq, userlogin, &trntlRes)
		if err != nil {
			return nil, err
		}
		if len(trntlRes) != 1 {
			l.Debug("Tarantool Select", "login not found in tarantool")
			return suckhttp.NewResponse(403, "Forbidden"), nil
		}
		//
		cookieData["userid"] = trntlRes[0].UserId

	} else {
		l.Error("Cookie", errors.New("login not specified in cookie")) // l.Error??
		return suckhttp.NewResponse(500, "Internal Server Error"), nil
	}

	var body []byte
	var contentType string
	if strings.Contains(r.GetHeader(suckhttp.Accept), "application/json") {
		body, err = json.Marshal(cookieData)
		if err != nil {
			l.Error("Marshalling decoded data", err)
			return suckhttp.NewResponse(500, "Internal Server Error"), nil
		}
		contentType = "application/json"
	} else if strings.Contains(r.GetHeader(suckhttp.Accept), "text/plain") {
		if login, ok := cookieData["userid"]; ok {
			body = []byte(login.(string))
		} else {
			return suckhttp.NewResponse(500, "Internal Server Error"), nil
		}
		contentType = "text/plain"
	} else {
		l.Debug("Accept", "not allowed")
		return suckhttp.NewResponse(400, "Bad request"), nil
	}

	return suckhttp.NewResponse(200, "OK").AddHeader(suckhttp.Content_Type, contentType).SetBody(body), nil

}
