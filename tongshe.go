package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/gorilla/mux"
	"github.com/skratchdot/open-golang/open"
)

var Token string

func getConfigFunc(name string) func(string, string) {
	if name == "is_global" {
		return func(name, value string) {
			refreshGlobalToggle(value == "on")
			SetPac()
		}
	}
	if name == "child_lock" {
		return func(name, value string) {
			SetPac()
		}
	}
	return func(s, v string) {}
}

type JsonResponse struct {
	Succeed bool             `json:"ok"`
	Data    *json.RawMessage `json:"data"`
	Message string           `json:"message"`
}

func renderJson(w http.ResponseWriter, res *JsonResponse) {
	buf, err := json.Marshal(res)
	if err != nil {
		fmt.Fprintf(w, "json marshal failed with error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(buf)
}

func set(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	value := r.FormValue("value")
	if err := effectSetting(name, value); err != nil {
		res := &JsonResponse{Succeed: false, Data: nil, Message: "设置文件有问题"}
		renderJson(w, res)
		return
	}
	settings(w, r)
}

func effectSetting(name, value string) error {
	log.Printf("Get command to set %s to %s", name, value)
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	config.Set(name, value)
	f := getConfigFunc(name)
	f(name, value)
	return nil
}

func settings(w http.ResponseWriter, r *http.Request) {
	config, _ := LoadConfig()
	bt, _ := json.Marshal(config.Config)
	data := (*json.RawMessage)(&bt)
	res := &JsonResponse{
		Succeed: true, Data: data, Message: ""}
	renderJson(w, res)
}

func getPac(w http.ResponseWriter, r *http.Request) {
	bt := GetRes("pac.tpl")
	var proxy string
	proxy = fmt.Sprintf("PROXY %s;DIRECT;", HttpProxy)
	proxy = fmt.Sprintf(
		"SOCKS5 %s; SOCKS %s; DIRECT;",
		SocksProxy,
		SocksProxy)
	// TODO
	config, _ := LoadConfig()
	dds := config.Get("diy_domains")
	dms := []string{}
	if len(dds) > 0 {
		dms = strings.Split(dds, ",")
	}
	dmsJson, _ := json.Marshal(dms)

	s := strings.Replace(string(bt), "__PROXY__", proxy, -1)
	s = strings.Replace(s, "__DOMAINS__", string(dmsJson), -1)
	isGs := "false"

	global := config.Get("is_global") == "on"
	if global {
		isGs = "true"
	}
	s = strings.Replace(s, "__IS_GLOBAL__", isGs, -1)
	fmt.Fprintf(w, s)
}

func tokenRequired(f func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil || cookie.Value != Token {
			log.Printf("token is not correct, get:%s ", cookie)
			res := &JsonResponse{Succeed: false, Data: nil, Message: "Unknown source"}
			renderJson(w, res)
		} else {
			f(w, r)
		}
	}
}

func static(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/index" || p == "/" || p == "/index.html" {
		http.SetCookie(w, &http.Cookie{Name: "token", Value: Token, Path: "/"})
		p = "/index.html"
	}
	f := "ui" + p
	fmt.Println(f)
	arr := strings.Split(f, ".")
	switch arr[len(arr)-1] {
	case "css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case "js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Write(GetRes(f))
}

func RandomString(strlen int) string {
	rand.Seed(time.Now().UTC().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func shadowsocks(w http.ResponseWriter, r *http.Request) {
	config, err := LoadConfig()
	if err != nil {
	}
	switch r.Method {
	case "POST":
		ss := r.FormValue("ss")
		log.Printf("Post ss with value: %s", ss)
		err = config.AddTunnel(ss)
	case "DELETE":
		ss := r.URL.Query().Get("ss")
		log.Printf("Delete ss: %s", ss)
		err = config.DeleteTunnel(ss)
	case "PUT":
		ss := r.FormValue("ss")
		old := r.URL.Query().Get("ss")
		err = config.UpdateTunnel(old, ss)
	}
	log.Printf("ss tunnels count is %d now", len(config.GetSSTunnels()))
	SetTunnels(config.GetSSTunnels())
	if len(config.GetSSTunnels()) == 0 {
		UnsetPac()
	}
	if r.Method == "POST" && len(config.GetSSTunnels()) == 1 {
		SetPac()
	}
	bt, _ := json.Marshal(config.SSTunnels)
	data := (*json.RawMessage)(&bt)
	if err == nil {
		res := &JsonResponse{Succeed: true, Data: data, Message: ""}
		renderJson(w, res)
	} else {
		res := &JsonResponse{Succeed: false, Data: data, Message: err.Error()}
		renderJson(w, res)
	}
}

func StartWeb() {
	Token = RandomString(32)
	rtr := mux.NewRouter()
	rtr.HandleFunc("/pac", getPac)
	rtr.HandleFunc("/set", tokenRequired(set))
	rtr.HandleFunc("/settings", tokenRequired(settings))
	rtr.HandleFunc("/shadowsocks", tokenRequired(shadowsocks))
	rtr.PathPrefix("/").HandlerFunc(static)
	http.Handle("/", rtr)
	srv := &http.Server{
		Handler:      rtr,
		Addr:         GetManagementAddr(),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	log.Printf("start web at : %s", GetManagementAddr())
	log.Fatal(srv.ListenAndServe())
}

func onTrayReady() {
	// iconBytes should be the []byte content of .ico for windows and .ico/.jpg/.png
	// for other platforms.

	setrlimit()

	systray.SetIcon(GetRes("icon22x22.ico"))
	systray.SetTitle("")
	systray.SetTooltip("铜蛇")

	go StartSS()
	go StartHttpProxy()

	config, _ := LoadConfig()
	SetTunnels(config.GetSSTunnels())
	SetPac()
	go traceTray()
	StartWeb()
}

func setrlimit() {
	var rl syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl)
	if err != nil {
		log.Printf("get rlimit error: %v", err)
	}
	rl.Cur = 99999
	rl.Max = 99999
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rl)
	if err != nil {
		log.Printf("set rlimit error: %v", err)
	}
}

var mToggleGlobal *systray.MenuItem

func refreshGlobalToggle(global bool) {
	if global {
		mToggleGlobal.SetTitle("关闭全局模式")
	} else {
		mToggleGlobal.SetTitle("开启全局模式")
	}
}

func traceTray() {

	mConfig := systray.AddMenuItem("账号与设置", "账号与设置")
	mToggleGlobal = systray.AddMenuItem("开启全局模式", "全局模式开关")
	mQuit := systray.AddMenuItem("退出", "退出铜蛇")

	murl := fmt.Sprintf("http://%s", GetManagementAddr())
	open.Run(murl)

	config, _ := LoadConfig()

	var global bool = config.Get("is_global") == "on"
	refreshGlobalToggle(global)

	for {
		select {
		case <-mConfig.ClickedCh:
			open.Run(murl)
		case <-mQuit.ClickedCh:
			log.Println("clear pac settings...")
			UnsetPac()
			log.Println("sync rest traffic ...")
			TrafficCounter.Sync()
			log.Println("shut tray...")
			systray.Quit()
			log.Println("Quit...")
			os.Exit(0)
			return
		case <-mToggleGlobal.ClickedCh:
			config, _ := LoadConfig()
			global := !(config.Get("is_global") == "on")
			if global {
				effectSetting("is_global", "on")
			} else {
				effectSetting("is_global", "off")
			}
			refreshGlobalToggle(global)
			log.Printf("global mode is %v ", global)
		}
	}
}

func main() {
	systray.Run(onTrayReady)
}
