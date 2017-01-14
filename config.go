package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ss_go "github.com/dawei101/shadowsocks-go/shadowsocks"
	"github.com/getlantern/pac"
)

const debug = true

const (
	Logo           = "logo.png"
	AppName        = "tongshe"
	ssPort         = 1271
	httpProxyPort  = 1272
	httpManagePort = 1270
	HttpProxy      = "127.0.0.1:1272"
	SocksProxy     = "127.0.0.1:1271"
)

var storageFolder string
var cacheFolder string

func init() {
	if runtime.GOOS == "darwin" {
		storageFolder = os.Getenv("HOME") + "/Library/Application Support"
		cacheFolder = os.Getenv("HOME") + "/Library/Caches"
	} else if runtime.GOOS == "windows" {
		storageFolder = os.Getenv("APPDATA")
		cacheFolder = os.Getenv("LOCALAPPDATA")
	} else {
		if os.Getenv("XDG_CONFIG_HOME") != "" {
			storageFolder = os.Getenv("XDG_CONFIG_HOME")
		} else {
			storageFolder = filepath.Join(os.Getenv("HOME"), ".config")
		}
		if os.Getenv("XDG_CACHE_HOME") != "" {
			cacheFolder = os.Getenv("XDG_CACHE_HOME")
		} else {
			cacheFolder = filepath.Join(os.Getenv("HOME"), ".cache")
		}
	}
	s := GetStorageDir()
	if !isPathExist(s) {
		os.Mkdir(s, 0755)
	}
	if !isPathExist(GetStorageFile(Logo)) {
		err := ioutil.WriteFile(GetStorageFile(Logo), GetRes(Logo), 0644)
		if err != nil {
			panic(err)
		}
	}
}

type SSTunnel struct {
	Ip       string
	Port     string
	Password string
	Method   string
}

func (ss *SSTunnel) ToString() string {
	return fmt.Sprintf("ss://%s:%s@%s:%s", ss.Method, ss.Password, ss.Ip, ss.Port)
}

func NewSSTunnel(ss string) (*SSTunnel, error) {
	re, err := regexp.Compile(`(ss)://([\d\-\w]+):([\d\w]+)\@([\d\w\.]+):(\d+)`)
	if err != nil {
		log.Printf("regexp for ss tunnel is not correct")
		os.Exit(-1)
	}
	res := re.FindStringSubmatch(ss)
	if res != nil {
		method, password, ip, port := res[2], res[3], res[4], res[5]
		if err = ss_go.CheckCipherMethod(strings.Replace(method, "-auth", "", 1)); err != nil {
			return nil, errors.New("不支持的加密类型:" + method)
		}
		return &SSTunnel{ip, port, password, method}, nil
	}
	return nil, errors.New("输入不是shadowsocks格式")
}

type Timestamp time.Time

func (t *Timestamp) MarshalJSON() ([]byte, error) {
	ts := time.Time(*t).Unix()
	stamp := fmt.Sprint(ts)

	return []byte(stamp), nil
}

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	ts, err := strconv.Atoi(string(b))
	if err != nil {
		return err
	}

	*t = Timestamp(time.Unix(int64(ts), 0))

	return nil
}

type Traffic struct {
	Month string `json:"month"`
	In    int64  `json:"in"`
	Out   int64  `json:"out"`
}

type Config struct {
	SSTunnels []string          `json:"ss_tunnels"`
	Config    map[string]string `json:"config"`
	Traffic   *Traffic          `json:"traffic"`
}

func (c *Config) Set(name string, value string) {
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	c.Config[name] = value
	SaveConfig(c)
}

func (c *Config) Get(name string) string {
	v, ok := c.Config[name]
	if ok {
		return v
	}
	return ""
}

func (c *Config) AddTraffic(in, out int64) {
	t := time.Now().UTC()
	curMonth := fmt.Sprintf("%d%d", t.Year(), t.Month())
	if c.Traffic == nil || c.Traffic.Month != curMonth {
		c.Traffic = &Traffic{curMonth, 0, 0}
	}
	atomic.AddInt64(&c.Traffic.In, in)
	atomic.AddInt64(&c.Traffic.Out, out)
}

func (c *Config) GetTraffic() *Traffic {
	t := time.Now().UTC()
	curMonth := fmt.Sprintf("%s%s", t.Year(), t.Month())
	if c.Traffic != nil && c.Traffic.Month == curMonth {
		return c.Traffic
	}
	return &Traffic{curMonth, 0, 0}
}

func (c *Config) GetSSTunnels() []*SSTunnel {
	tunnels := []*SSTunnel{}
	for _, sv := range c.SSTunnels {
		ss, err := NewSSTunnel(sv)
		if err == nil {
			tunnels = append(tunnels, ss)
		}
	}
	return tunnels
}

func (c *Config) AddTunnel(t string) error {
	_, err := NewSSTunnel(t)
	if err != nil {
		return err
	}
	for _, sv := range c.SSTunnels {
		if sv == t {
			return errors.New("该Shadowsocks账号已存在")
		}
	}
	c.SSTunnels = append(c.SSTunnels, t)
	return SaveConfig(c)
}

func (c *Config) UpdateTunnel(oldT, newT string) error {
	_, err := NewSSTunnel(newT)
	if err != nil {
		return err
	}
	sure := true
	for i, sv := range c.SSTunnels {
		if sv == oldT {
			c.SSTunnels[i] = newT
		}
		if sv == newT {
			sure = false
		}
	}
	if sure {
		return SaveConfig(c)
	}
	return errors.New("该Shadowsocks账号已存在")
}

func (c *Config) DeleteTunnel(t string) error {
	for i, sv := range c.SSTunnels {
		if sv == t {
			c.SSTunnels = append(c.SSTunnels[:i], c.SSTunnels[i+1:]...)
			break
		}
	}
	return SaveConfig(c)
}

var configMutex = &sync.RWMutex{}

func LoadConfig() (*Config, error) {
	config := &Config{}
	f := fmt.Sprintf("%s/%s", GetStorageDir(), "user.config")

	//log.Printf("read lock on config file ")
	configMutex.RLock()
	c, err := ioutil.ReadFile(f)
	configMutex.RUnlock()
	//log.Printf("read lock on config file released")
	if err != nil || len(c) == 0 {
		config = &Config{
			[]string{},
			map[string]string{},
			&Traffic{"201605", 0, 0},
		}
		SaveConfig(config)
		log.Printf("read config file err:%v", err)
		return config, nil
	}
	if err = json.Unmarshal(c, config); err != nil {
		log.Printf("dejson config err:%v", err)
		SaveConfig(config)
	}
	return config, nil
}

func SaveConfig(config *Config) error {
	f := fmt.Sprintf("%s/%s", GetStorageDir(), "user.config")
	b, err := json.Marshal(&config)
	if err != nil {
		log.Printf("enjson config err:%v", err)
		return err
	}
	//log.Printf("write lock on config file ")
	configMutex.Lock()
	defer func() {
		configMutex.Unlock()
		//log.Printf("write lock on config file released")
	}()

	if err = ioutil.WriteFile(f, b, 0644); err != nil {
		log.Printf("write config err:%v", err)
		return err
	}
	//log.Printf("save config succeed")
	return nil
}

func GetRes(filename string) []byte {
	bt, err := Asset(filename)
	if err != nil {
		return []byte{}
	}
	return bt
}

func GetStorageDir() string {
	return fmt.Sprintf("%s/%s", storageFolder, AppName)
}

func GetStorageFile(f string) string {
	return fmt.Sprintf("%s/%s", GetStorageDir(), f)
}

func isPathExist(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

var pacUrl string

func SetPac() error {
	err := pac.EnsureHelperToolPresent(
		GetStorageFile("pac-set"),
		"铜蛇请求授权，以更改系统代理设置",
		GetStorageFile(Logo),
	)
	if err != nil {
		log.Printf("Could not set pac with err: %v", err)
		os.Exit(1)
	}

	config, _ := LoadConfig()
	if len(config.GetSSTunnels()) == 0 {
		return errors.New("无Shadowsocks账号, pac不打开")
	}
	pacUrl = fmt.Sprintf("http://%s/pac?%d", GetManagementAddr(), time.Now().Nanosecond())
	return pac.On(pacUrl)
}

func UnsetPac() {
	pac.Off(pacUrl)
}

func GetSocksProxy() string {
	//TODO add sharing feature
	return fmt.Sprintf("%s:%d;", "127.0.0.1", ssPort)
}

func GetHttpProxy() string {
	//TODO add sharing feature
	return fmt.Sprintf("%s:%d;", "127.0.0.1", httpProxyPort)
}

func GetManagementAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", httpManagePort)
}
