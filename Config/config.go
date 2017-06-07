package Config

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-ini/ini"

	"github.com/Djoulzy/Polycom/CLog"
)

type ServerID struct {
	Name string
}

type Globals struct {
	LogLevel     int
	StartLogging bool
}

type ConnectionLimit struct {
	MaxUsersConns     int
	MaxMonitorsConns  int
	MaxServersConns   int
	MaxIncommingConns int
}

type ServersAddresses struct {
	HTTPaddr string
	TCPaddr  string
}

type KnownBrothers struct {
	Servers map[string]string
}

type HTTPServerConfig struct {
	ReadBufferSize   int
	WriteBufferSize  int
	HandshakeTimeout int
}

type TCPServerConfig struct {
	ConnectTimeOut           int
	WriteTimeOut             int
	ScalingCheckServerPeriod int
}

type Encryption struct {
	HASH_SIZE int
	HEX_KEY   string
	HEX_IV    string
}

type Data struct {
	ServerID
	Globals
	ConnectionLimit
	ServersAddresses
	KnownBrothers
	HTTPServerConfig
	TCPServerConfig
	Encryption
}

func Load() (*Data, error) {
	conf := &Data{
		ServerID{},
		Globals{
			LogLevel:     4,
			StartLogging: true,
		},
		ConnectionLimit{
			MaxUsersConns:     100,
			MaxMonitorsConns:  3,
			MaxServersConns:   5,
			MaxIncommingConns: 50,
		},
		ServersAddresses{},
		KnownBrothers{},
		HTTPServerConfig{
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			HandshakeTimeout: 5,
		},
		TCPServerConfig{
			ConnectTimeOut:           2,
			WriteTimeOut:             1,
			ScalingCheckServerPeriod: 10,
		},
		Encryption{
			HASH_SIZE: 8,
			HEX_KEY:   "0000000000000000000000000000000000000000000000000000000000000000",
			HEX_IV:    "00000000000000000000000000000000",
		},
	}

	conf.HTTPaddr = *flag.String("httpaddr", "localhost:8080", "http service address")
	conf.TCPaddr = *flag.String("tcpaddr", "localhost:8081", "tcp service address")
	flag.Parse()

	cfg, err := ini.Load(fmt.Sprintf("%s/etc/server.ini", os.Getenv("GOPATH")))
	if err != nil {
		clog.Error("server", "getConf", "Invalid conf file: %s", err)
		return conf, err
	}
	err = cfg.MapTo(conf)

	sec1, err := cfg.GetSection("KnownBrothers")
	if err == nil {
		conf.KnownBrothers.Servers = sec1.KeysHash()
	}
	return conf, err
}