package main

import (
	"errors"
	"project/connector"
	"project/types/configuratortypes"
	"project/types/netprotocol"
	"strconv"
	"strings"

	"github.com/big-larry/suckutils"
)

func (s *service) NewMessage() connector.MessageReader {
	return connector.NewBasicMessage()
}

func (s *service) Handle(message connector.MessageReader) error {

	payload := message.(*connector.BasicMessage).Payload
	if len(payload) == 0 {
		return connector.ErrEmptyPayload
	}
	switch configuratortypes.OperationCode(payload[0]) {
	case configuratortypes.OperationCodePing:
		s.l.Debug("New message", "OperationCodePing")
		return nil
	case configuratortypes.OperationCodeGiveMeOuterAddr:
		s.l.Debug("New message", "OperationCodeGiveMeOuterAddr")
		if netw, addr, err := s.outerAddr.getListeningAddr(); err != nil {
			return errors.New(suckutils.ConcatTwo("getlisteningaddr err: ", err.Error()))
		} else {
			formatted_addr := configuratortypes.FormatAddress(netw, addr)
			if err := s.connector.Send(connector.FormatBasicMessage(append(append(make([]byte, 0, len(formatted_addr)+2), byte(configuratortypes.OperationCodeSetOutsideAddr), byte(len(formatted_addr))), formatted_addr...))); err != nil {
				return err
			}
			return nil
		}
	case configuratortypes.OperationCodeSubscribeToServices:
		s.l.Debug("New message", "OperationCodeSubscribeToServices")
		raw_pubnames := configuratortypes.SeparatePayload(payload[1:])
		if raw_pubnames == nil {
			return connector.ErrWeirdData
		}
		pubnames := make([]ServiceName, 0, len(raw_pubnames))
		for _, raw_pubname := range raw_pubnames {
			if len(raw_pubname) == 0 {
				return connector.ErrWeirdData
			}
			pubnames = append(pubnames, ServiceName(raw_pubname))
		}
		return s.subs.subscribe(s, pubnames...)

	case configuratortypes.OperationCodeUpdatePubs:
		s.l.Debug("New message", "OperationCodeUpdatePubs")
		if s.name == ServiceName(configuratortypes.ConfServiceName) {
			updates := configuratortypes.SeparatePayload(payload[1:])
			if len(updates) != 0 {
				foo := s.connector.RemoteAddr().String()
				external_ip := (foo)[:strings.Index(foo, ":")]
				for _, update := range updates {
					pubname, raw_addr, status, err := configuratortypes.UnformatOpcodeUpdatePubMessage(update)
					if err != nil {
						s.l.Error("UnformatOpcodeUpdatePubMessage", err)
						return connector.ErrWeirdData
					}
					netw, addr, err := configuratortypes.UnformatAddress(raw_addr)
					if err != nil {
						s.l.Error("UnformatAddress", err)
						return connector.ErrWeirdData
					}
					switch netw {
					case netprotocol.NetProtocolUnix:
						netw = netprotocol.NetProtocolNonlocalUnix
					case netprotocol.NetProtocolTcp:
						if (addr)[:strings.Index(addr, ":")] == "127.0.0.1" {
							addr = suckutils.ConcatTwo(external_ip, (addr)[strings.Index(addr, ":"):])
						}
					}
					s.subs.updatePub(pubname, configuratortypes.FormatAddress(netw, addr), status, false)
				}
			}
		} else {
			return errors.New("not configurator, but sent OperationCodeUpdatePubs")
		}
	case configuratortypes.OperationCodeMyStatusChanged:
		s.l.Debug("New message", "OperationCodeMyStatusChanged")
		if len(payload) < 2 {
			return connector.ErrWeirdData
		}
		s.changeStatus(configuratortypes.ServiceStatus(payload[1]))
	case configuratortypes.OperationCodeMyOuterPort:
		s.l.Debug("New message", "OperationCodeMyOuterPort")
		if s.name == ServiceName(configuratortypes.ConfServiceName) {
			if len(payload) < 2 {
				return connector.ErrWeirdData
			}
			if p, err := strconv.Atoi(string(payload[1:])); err != nil || p == 0 {
				return connector.ErrWeirdData
			} else {
				s.statusmux.Lock()
				s.outerAddr.port = string(payload)
				s.statusmux.Unlock()
			}
		} else {
			return errors.New("not configurator, but sent OperationCodeMyOuterPort")
		}
	default:
		return connector.ErrWeirdData
	}
	return nil
}

func (s *service) HandleClose(reason error) {
	s.l.Warning("Connection", suckutils.ConcatFour("with \"", string(s.name), "\" closed, reason err: ", reason.Error()))
	s.changeStatus(configuratortypes.StatusOff)

	if s.name == ServiceName(configuratortypes.ConfServiceName) {
		reconnectReq <- s
	}
}
