package rtc

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/ion/pkg/log"
	"github.com/pion/ion/pkg/rtc/plugins"
	"github.com/pion/ion/pkg/rtc/rtpengine"
	"github.com/pion/ion/pkg/rtc/transport"
)

const (
	statCycle    = 3 * time.Second
	maxCleanSize = 100
)

var (
	routers    = make(map[string]*Router)
	routerLock sync.RWMutex

	//CleanChannel return the dead pub's mid
	CleanChannel = make(chan string, maxCleanSize)

	stop bool
)

// Init port and ice urls
func Init(port int, ices []string, kcpKey, kcpSalt string) error {

	//init ice urls and trickle-ICE
	transport.InitWebRTC(ices, true, false)

	// show stat about all pipelines
	go check()

	var connCh chan *transport.RTPTransport
	var err error
	// accept relay rtptransport
	if kcpKey != "" && kcpSalt != "" {
		connCh, err = rtpengine.ServeWithKCP(port, kcpKey, kcpSalt)
	} else {
		connCh, err = rtpengine.Serve(port)
	}
	if err != nil {
		log.Errorf("rtc.Init err=%v", err)
		return err
	}
	go func() {
		for {
			if stop {
				return
			}
			select {
			case rtpTransport := <-connCh:
				id := rtpTransport.ID()
				cnt := 0
				for id == "" && cnt < 100 {
					id = rtpTransport.ID()
					time.Sleep(time.Millisecond)
					cnt++
				}
				if id == "" && cnt >= 100 {
					log.Errorf("invalid id from incoming rtp transport")
					return
				}
				log.Infof("accept new rtp id=%s conn=%s", id, rtpTransport.RemoteAddr().String())
				if router := AddRouter(id); router != nil {
					router.AddPub(id, rtpTransport)
				}
			}
		}
	}()
	return nil
}

// GetOrNewRouter get router from map
func GetOrNewRouter(id string) *Router {
	log.Infof("rtc.GetOrNewRouter id=%s", id)
	router := GetRouter(id)
	if router == nil {
		return AddRouter(id)
	}
	return router
}

// GetRouter get router from map
func GetRouter(id string) *Router {
	log.Infof("rtc.GetRouter id=%s", id)
	routerLock.RLock()
	defer routerLock.RUnlock()
	return routers[id]
}

// AddRouter add a new router
func AddRouter(id string) *Router {
	log.Infof("rtc.AddRouter id=%s", id)
	routerLock.Lock()
	defer routerLock.Unlock()
	routers[id] = NewRouter(id)
	return routers[id]
}

// DelRouter delete pub
func DelRouter(id string) {
	log.Infof("DelRouter id=%s", id)
	router := GetRouter(id)
	if router == nil {
		return
	}
	router.Close()
	routerLock.Lock()
	defer routerLock.Unlock()
	delete(routers, id)
}

// Close close all Router
func Close() {
	if stop {
		return
	}
	stop = true
	routerLock.Lock()
	defer routerLock.Unlock()
	for id, router := range routers {
		if router != nil {
			router.Close()
			delete(routers, id)
		}
	}
}

// check show all Routers' stat
func check() {
	t := time.NewTicker(statCycle)
	for {
		select {
		case <-t.C:
			info := "\n----------------rtc-----------------\n"
			print := false
			routerLock.Lock()
			if len(routers) > 0 {
				print = true
			}

			for id, Router := range routers {
				if !Router.Alive() {
					Router.Close()
					delete(routers, id)
					CleanChannel <- id
					log.Infof("Stat delete %v", id)
				}
				info += "pub: " + id + "\n"
				info += Router.GetPlugin(jbPlugin).(*plugins.JitterBuffer).Stat()
				subs := Router.GetSubs()
				if len(subs) < 6 {
					for id := range subs {
						info += fmt.Sprintf("sub: %s\n\n", id)
					}
				} else {
					info += fmt.Sprintf("subs: %d\n\n", len(subs))
				}
			}
			routerLock.Unlock()
			if print {
				log.Infof(info)
			}
		}
	}
}
