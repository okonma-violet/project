package wsservice

import (
	"context"
	"errors"
	"net"

	"project/connector"
	"project/logs/logger"
	"project/types/configuratortypes"
	"project/types/netprotocol"

	"strconv"
	"strings"
	"time"

	"github.com/big-larry/suckutils"
)

type configurator struct {
	conn            *connector.EpollReConnector
	thisServiceName ServiceName

	publishers *publishers
	listener   *listener
	servStatus *serviceStatus

	terminationByConfigurator chan struct{}
	l                         logger.Logger
}

func newFakeConfigurator(ctx context.Context, listenport int, l logger.Logger, servStatus *serviceStatus, listener *listener) *configurator {
	connector.InitReconnection(ctx, time.Second*5, 1, 1)
	c := &configurator{
		l:          l,
		servStatus: servStatus,
		listener:   listener,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		c.l.Error("newFakeConfigurator", err)
		return nil
	}
	go func() {
		if _, err := ln.Accept(); err != nil {
			panic("newFakeConfigurator/ln.Accept err:" + err.Error())
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		c.l.Error("newFakeConfigurator/Dial", err)
		return nil
	}

	if c.conn, err = connector.NewEpollReConnector(conn, c, nil, nil); err != nil {
		c.l.Error("NewEpollReConnector", err)
		return nil
	}
	var randport bool
	if listenport == 0 {
		randport = true
		listenport = 9010
	}
	go func() {
		for {
			time.Sleep(time.Millisecond * 50)
			addr := configuratortypes.FormatAddress(netprotocol.NetProtocolTcp, "127.0.0.1:"+strconv.Itoa(listenport))
			if err := c.Handle(&connector.BasicMessage{Payload: append(append(make([]byte, 0, len(addr)+2), byte(configuratortypes.OperationCodeSetOutsideAddr), byte(len(addr))), addr...)}); err != nil {
				c.l.Error("FakeConfiguratorsMessageHandle", err)
				if randport {
					listenport++
				} else {
					panic(err)
				}
				continue
			}
			if c.servStatus.isListenerOK() {
				return
			}
		}
	}()
	return c
}

func newConfigurator(ctx context.Context, l logger.Logger, servStatus *serviceStatus, pubs *publishers, listener *listener, configuratoraddr string, thisServiceName ServiceName, reconnectTimeout time.Duration) *configurator {

	c := &configurator{
		thisServiceName:           thisServiceName,
		l:                         l,
		servStatus:                servStatus,
		publishers:                pubs,
		listener:                  listener,
		terminationByConfigurator: make(chan struct{}, 1)}

	connector.InitReconnection(ctx, reconnectTimeout, 1, 1)

	go func() {
		for {
			conn, err := net.Dial((configuratoraddr)[:strings.Index(configuratoraddr, ":")], (configuratoraddr)[strings.Index(configuratoraddr, ":")+1:])
			if err != nil {
				l.Error("Dial", err)
				goto timeout
			}

			if err = c.handshake(conn); err != nil {
				conn.Close()
				l.Error("handshake", err)
				goto timeout
			}
			if c.conn, err = connector.NewEpollReConnector(conn, c, c.handshake, c.afterConnProc); err != nil {
				l.Error("NewEpollReConnector", err)
				goto timeout
			}
			if err = c.conn.StartServing(); err != nil {
				c.conn.ClearFromCache()
				l.Error("StartServing", err)
				goto timeout
			}
			if err = c.afterConnProc(); err != nil {
				c.conn.Close(err)
				l.Error("afterConnProc", err)
				goto timeout
			}
			c.l.Debug("Conn", "First connection was successful")
			break
		timeout:
			l.Debug("First connection", "failed, timeout")
			time.Sleep(reconnectTimeout)
		}
	}()

	return c
}

func (c *configurator) handshake(conn net.Conn) error {
	if _, err := conn.Write(connector.FormatBasicMessage([]byte(c.thisServiceName))); err != nil {
		return err
	}
	buf := make([]byte, 5)
	conn.SetReadDeadline(time.Now().Add(time.Second * 2))
	n, err := conn.Read(buf)
	if err != nil {
		return errors.New(suckutils.ConcatTwo("err reading configurator's approving, err: ", err.Error()))
	}
	if n == 5 {
		if buf[4] == byte(configuratortypes.OperationCodeOK) {
			c.l.Debug("Conn", "handshake passed")
			return nil
		} else if buf[4] == byte(configuratortypes.OperationCodeNOTOK) {
			if c.conn != nil {
				go c.conn.CancelReconnect() // горутина пушто этот хэндшейк под залоченным мьютексом выполняется
			}
			c.terminationByConfigurator <- struct{}{}
			return errors.New("configurator do not approve this service")
		}
	}
	return errors.New("configurator's approving format not supported or weird")
}

func (c *configurator) afterConnProc() error {

	myStatus := byte(configuratortypes.StatusSuspended)
	if c.servStatus.onAir() {
		myStatus = byte(configuratortypes.StatusOn)
	}
	if err := c.conn.Send(connector.FormatBasicMessage([]byte{byte(configuratortypes.OperationCodeMyStatusChanged), myStatus})); err != nil {
		return err
	}

	if c.publishers != nil {
		pubnames := c.publishers.GetAllPubNames()
		if len(pubnames) != 0 {
			message := append(make([]byte, 0, len(pubnames)*15), byte(configuratortypes.OperationCodeSubscribeToServices))
			for _, pub_name := range pubnames {
				pub_name_byte := []byte(pub_name)
				message = append(append(message, byte(len(pub_name_byte))), pub_name_byte...)
			}
			if err := c.conn.Send(connector.FormatBasicMessage(message)); err != nil {
				return err
			}
		}
	}

	if err := c.conn.Send(connector.FormatBasicMessage([]byte{byte(configuratortypes.OperationCodeGiveMeOuterAddr)})); err != nil {
		return err
	}
	c.l.Debug("Conn", "afterConnProc passed")
	return nil
}

func (c *configurator) send(message []byte) error {
	if c == nil {
		return errors.New("nil configurator")
	}
	if c.conn == nil {
		return connector.ErrNilConn
	}
	if c.conn.IsClosed() {
		return connector.ErrClosedConnector
	}
	if err := c.conn.Send(message); err != nil {
		c.conn.Close(err)
		return err
	}
	return nil
}

func (c *configurator) onSuspend(reason string) {
	c.l.Warning("OwnStatus", suckutils.ConcatTwo("suspended, reason: ", reason))
	c.send(connector.FormatBasicMessage([]byte{byte(configuratortypes.OperationCodeMyStatusChanged), byte(configuratortypes.StatusSuspended)}))
}

func (c *configurator) onUnSuspend() {
	c.l.Warning("OwnStatus", "unsuspended")
	c.send(connector.FormatBasicMessage([]byte{byte(configuratortypes.OperationCodeMyStatusChanged), byte(configuratortypes.StatusOn)}))
}

func (c *configurator) NewMessage() connector.MessageReader {
	return connector.NewBasicMessage()
}

func (c *configurator) Handle(message interface{}) error {
	payload := message.(*connector.BasicMessage).Payload
	if len(payload) == 0 {
		return connector.ErrEmptyPayload
	}
	c.l.Debug("Handle/NewMessage", configuratortypes.OperationCode(payload[0]).String()) ////////////////////////////////////////
	switch configuratortypes.OperationCode(payload[0]) {
	case configuratortypes.OperationCodePing:
		return nil
	case configuratortypes.OperationCodeMyStatusChanged:
		return nil
	case configuratortypes.OperationCodeImSupended:
		return nil
	case configuratortypes.OperationCodeSetOutsideAddr:
		if len(payload) < 2 {
			return connector.ErrWeirdData
		}
		if len(payload) < 2+int(payload[1]) {
			return connector.ErrWeirdData
		}
		if netw, addr, err := configuratortypes.UnformatAddress(payload[2 : 2+int(payload[1])]); err != nil {
			return err
		} else {
			if netw == netprotocol.NetProtocolNil {
				c.listener.stop()
				c.servStatus.setListenerStatus(true)
				return nil
			}
			if cur_netw, cur_addr := c.listener.Addr(); cur_addr == addr && cur_netw == netw.String() {
				return nil
			}
			var err error
			for i := 0; i < 3; i++ {
				if err = c.listener.listen(netw.String(), addr); err != nil {
					c.listener.l.Error("listen", err)
					time.Sleep(time.Second)
				} else {
					return nil
				}
			}
			return err
		}
	case configuratortypes.OperationCodeUpdatePubs:
		updates := configuratortypes.SeparatePayload(payload[1:])
		if len(updates) != 0 {
			for _, update := range updates {
				pubname, raw_addr, status, err := configuratortypes.UnformatOpcodeUpdatePubMessage(update)
				if err != nil {
					return err
				}
				if c.publishers == nil {
					c.l.Error("Handle/OperationCodeUpdatePubs", errors.New(suckutils.ConcatThree("service have no pubs, but recieved update for pub pub: \"", string(pubname), "\", sending unsubscription")))
					message := append(append(make([]byte, 0, 2+len(pubname)), byte(configuratortypes.OperationCodeUnsubscribeFromServices), byte(len(pubname))), pubname...)
					if err := c.send(connector.FormatBasicMessage(message)); err != nil {
						c.l.Error("Handle/configurator.send", err)
					}
					continue
				}
				netw, addr, err := configuratortypes.UnformatAddress(raw_addr)
				if err != nil {
					c.l.Error("Handle/OperationCodeUpdatePubs/UnformatAddress", err)
					return connector.ErrWeirdData
				}
				if netw == netprotocol.NetProtocolNonlocalUnix {
					continue // TODO:
				}

				c.publishers.update(ServiceName(pubname), netw.String(), addr, status)
				return nil
			}
		} else {
			return connector.ErrWeirdData
		}
	}
	return connector.ErrWeirdData
}

func (c *configurator) HandleClose(reason error) {
	c.l.Warning("Configurator", suckutils.ConcatTwo("conn closed, reason err: ", reason.Error()))
	// в суспенд не уходим, пока у нас есть паблишеры - нам пофиг
}
