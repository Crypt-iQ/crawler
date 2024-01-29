package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
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
	filePath := ""

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

	// todo: ensure no dupes
	for _, v := range sam.Addresses {
		// todo: semaphores
		handshake(v.Addr)
	}

	// theirNA.IP.String() + ":" + strconv.Itoa(int(theirNA.Port))
	// handshake("65.21.199.219:8333")
}

func handshake(theirAddr string) {
	conn, err := net.Dial("tcp", theirAddr)
	if err != nil {
		return
	}

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
		case <-time.After(time.Second * 2):
			conn.Close()
		}
	}()

	for {
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
				"feefilter.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
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
