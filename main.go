package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

type serializedKnownAddress struct {
	Addr        string
	Src         string
	Attempts    int
	TimeStamp   int64
	LastAttempt int64
	LastSuccess int64
	Services    wire.ServiceFlag
	SrcServices wire.ServiceFlag
}

type serializedAddrManager struct {
	Version      int
	Key          [32]byte
	Addresses    []*serializedKnownAddress
	NewBuckets   [1024][]string
	TriedBuckets [1024][]string
}

func main() {
	// - load addrs from file
	filePath := "peers.json"

	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return
	}
	r, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer r.Close()

	var sam serializedAddrManager
	dec := json.NewDecoder(r)
	err = dec.Decode(&sam)
	if err != nil {
		fmt.Printf("error decoding json: %v\n", err)
		return
	}

	fmt.Printf("done decoding: %d addrs\n", len(sam.Addresses))

	c := &crawler{}

	dupeMap := make(map[string]struct{})

	count := 0
	for _, v := range sam.Addresses {
		count++
		if count == 300 {
			c.wg.Wait()
			count = 0
		}

		if _, ok := dupeMap[v.Addr]; ok {
			continue
		}

		dupeMap[v.Addr] = struct{}{}

		c.wg.Add(1)
		go c.handshake(v.Addr)

	}
}

type crawler struct {
	wg sync.WaitGroup
}

func (c *crawler) handshake(theirAddr string) {
	defer c.wg.Done()

	deadline := time.Second * 2

	fmt.Printf("dialing:%v\n", theirAddr)

	var conn net.Conn
	var err error
	d := net.Dialer{Timeout: time.Second * 2}
	conn, err = d.Dial("tcp", theirAddr)
	if err != nil {
		return
	}

	fmt.Printf("conn to: %v\n", theirAddr)

	ourNA := &wire.NetAddress{
		Services: wire.SFNodeNetwork | wire.SFNodeWitness,
	}
	nonce := uint64(0)
	blockNum := 827984

	theirNA := &wire.NetAddress{}

	// - send version
	msgVersion := wire.NewMsgVersion(
		ourNA, theirNA, nonce, int32(blockNum),
	)
	msgVersion.Services = wire.SFNodeNetwork | wire.SFNodeWitness
	msgVersion.ProtocolVersion = 70016

	_, err = wire.WriteMessageWithEncodingN(
		conn, msgVersion, 70016, chaincfg.MainNetParams.Net, wire.WitnessEncoding,
	)
	if err != nil {
		fmt.Printf("send version error: %v\n", err)
		return
	}

	// - wait for version
	conn.SetReadDeadline(time.Now().Add(deadline))
	_, msg, _, err := wire.ReadMessageWithEncodingN(
		conn, 70016, chaincfg.MainNetParams.Net, wire.WitnessEncoding,
	)
	if _, ok := msg.(*wire.MsgVersion); !ok {
		fmt.Printf("received %T instead of version\n", msg)
		return
	}

	// - send verack
	_, err = wire.WriteMessageWithEncodingN(
		conn, wire.NewMsgVerAck(), 70016, chaincfg.MainNetParams.Net,
		wire.WitnessEncoding,
	)
	if err != nil {
		fmt.Printf("send verack error: %v\n", err)
		return
	}

	go func() {
		select {
		case <-time.After(deadline):
			conn.Close()
		}
	}()

	fileName := "feefilter.log"

	for {
		conn.SetReadDeadline(time.Now().Add(deadline))
		_, msg, _, err = wire.ReadMessageWithEncodingN(
			conn, 70016, chaincfg.MainNetParams.Net, wire.WitnessEncoding,
		)
		if err == wire.ErrUnknownMessage {
			continue
		}
		if err != nil {
			fmt.Printf("read error loop: %v\n", err)
			return
		}

		if filter, ok := msg.(*wire.MsgFeeFilter); ok {
			// output to file
			f, err := os.OpenFile(
				fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
			)
			if err != nil {
				panic(fmt.Errorf("open file err: %v", err))
			}
			feeFilterStr := strconv.Itoa(int(filter.MinFee)) + "\n"

			if _, err := f.Write([]byte(feeFilterStr)); err != nil {
				panic(fmt.Errorf("failed to write: %v", err))
			}

			if err := f.Close(); err != nil {
				panic(fmt.Errorf("failed to close: %v", err))
			}

			return
		}
	}
}
