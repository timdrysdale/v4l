// Package v4l, a facade to the Video4Linux video capture interface
// Copyright (C) 2016 Zoltán Korándi <korandi.z@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Command streamcam streams MJPEG from a V4L device over HTTP.
//
// Command Line
//
// Usage:
//   streamcam [flags]
//
// Flags:
//  -a string
//          address to listen on (default ":8080")
//  -d string
//          path to the capture device
//  -f int
//          frame rate
//  -h int
//          image height
//  -l
//          print supported device configs and quit
//  -r
//          reset all controls to default
//  -w int
//          image width
package main

import (

	"encoding/json" 
	"flag"
	"fmt"
	"html/template"

	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	//"time"

	"github.com/timdrysdale/v4l"
	"github.com/timdrysdale/v4l/fmt/h264"

	"github.com/gorilla/websocket" 



)

 
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1500000,
    WriteBufferSize: 1500000,
}

type Connection struct {  
    ws *websocket.Conn   
    send chan []byte 
    hub *Hub
    run bool
    key string  
    fileMP4DataName string      
    file264DataName string  
    file264SizeName string 
}

var fo *os.File
var gConn *Connection
var gHub *Hub 

func indexHandler(w http.ResponseWriter, r *http.Request) { 
    t, err := template.ParseFiles("./index.html")
    if err != nil {
        http.Error(w, err.Error(),
            http.StatusInternalServerError) 
        return
    }
    t.Execute(w,  "ws://"+r.Host+"/play")
}

func dist(w http.ResponseWriter, r *http.Request) { 
    http.ServeFile(w, r, "./"+r.URL.Path[1:])
}

func parseAVCNALu(array []byte) int { 
  arrayLen := len(array)
  i := 0
  state := 0
  count := 0
  for i < arrayLen {
    value := array[i];
    i += 1
    // finding 3 or 4-byte start codes (00 00 01 OR 00 00 00 01)
    switch state {
      case 0:
        if value == 0 {
          state = 1              
        }   
      case 1:
        if value == 0 {
          state = 2            
        } else {
          state = 0
        }        
      case 2,3:        
        if value == 0 {
          state = 3           
        } else if value == 1 && i < arrayLen {
          unitType := array[i] & 0x1f         
          if unitType == 7 || unitType == 8{
              count += 1
          } 
            state = 0
          } else {
            state = 0
          }    
      }       
    } 
  return count
}
func stream(cam *v4l.Device) {

	
	for {
		buf, err := cam.Capture()
		if err != nil {
			log.Println("Capture:", err)
			proc, _ := os.FindProcess(os.Getpid())
			proc.Signal(os.Interrupt)
			break
		}

		
		b := make([]byte, buf.Size())
		buf.ReadAt(b, 0)
		

		
	}
}

func (conn *Connection) app264Streaming(cam *v4l.Device) {
	 f, err := os.Create("./stream.h264")
	 if err != nil {
	 	fmt.Println(err)
	 	return
	}
	
	for {
		buf, err := cam.Capture()
		if err != nil {
			log.Println("Capture:", err)
			proc, _ := os.FindProcess(os.Getpid())
			proc.Signal(os.Interrupt)
			break
		}

		
		b := make([]byte, buf.Size())
		buf.ReadAt(b, 0)
		fmt.Println(buf.BytesUsed())		
		_, err = f.Write(b[0:buf.BytesUsed()])
		if err != nil {
			fmt.Println(err)
			f.Close()
			return
		}
		//time.Sleep(1 * time.Second)

		//err = conn.ws.WriteMessage(2,  b[0:buf.BytesUsed()])
		//if err != nil {
		//	fmt.Printf("conn.WriteMessage ERROR!!!\n")
		//	break
		//}
		//fmt.Println(buf.BytesUsed())	
		 
		//runtime.Gosched()
	}
}	
    


func (conn *Connection) appReadCommand2(cam *v4l.Device) {
        
	for {
        _, message, err := conn.ws.ReadMessage()
        if err != nil {    
            break
	}        
        u := map[string]interface{}{}   
        json.Unmarshal(message, &u)   
		if u["t"].(string) == "open"{
			fmt.Println("starting streaming")
            go conn.app264Streaming(cam)
        }
   
    }
    conn.ws.Close()
}


func play2(w http.ResponseWriter, r *http.Request, cam *v4l.Device) { 
 
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Print("upgrade:", err)
        return
    }
    defer ws.Close()

    c := &Connection{hub: gHub, send: make(chan []byte, 256), ws: ws, run: true } 
    
    c.hub.register <- c
    
    c.key = c.hub.setHubConnName( c )
	fmt.Println("in play2") 
    go c.appReadCommand2(cam)

    for c.run { runtime.Gosched() }

    fmt.Fprintf(w, "ok")
 }


func main() {
	var (
		d = flag.String("d", "", "path to the capture device")
		w = flag.Int("w", 0, "image width")
		h = flag.Int("h", 0, "image height")
		f = flag.Int("f", 0, "frame rate")
		//a = flag.String("a", ":8080", "address to listen on")
		l = flag.Bool("l", false, "print supported device configs and quit")
		r = flag.Bool("r", false, "reset all controls to default")
	)
	flag.Parse()

	if *d == "" {
		devs := v4l.FindDevices()
		if len(devs) != 1 {
			fmt.Fprintln(os.Stderr, "Use -d to select device.")
			for _, info := range devs {
				fmt.Fprintln(os.Stderr, " ", info.Path)
			}
			os.Exit(1)
		}
		*d = devs[0].Path
	}
	fmt.Fprintln(os.Stderr, "Using device", *d)
	cam, err := v4l.Open(*d)
	fatal("Open", err)

	if *l {
		configs, err := cam.ListConfigs()
		fatal("ListConfigs", err)
		fmt.Fprintln(os.Stderr, "Supported device configs:")
		found := false
		for _, cfg := range configs {
			if cfg.Format != h264.FourCC {
				continue
			}
			found = true
			fmt.Fprintln(os.Stderr, " ", cfg2str(cfg))
		}
		if !found {
			fmt.Fprintln(os.Stderr, "  (none)")
		}
		os.Exit(0)
	}

	cfg, err := cam.GetConfig()
	fatal("GetConfig", err)
	cfg.Format = h264.FourCC
	if *w > 0 {
		cfg.Width = *w
	}
	if *h > 0 {
		cfg.Height = *h
	}
	if *f > 0 {
		cfg.FPS = v4l.Frac{uint32(*f), 1}
	}
	fmt.Fprintln(os.Stderr, "Requested config:", cfg2str(cfg))
	err = cam.SetConfig(cfg)
	fatal("SetConfig", err)
	err = cam.TurnOn()
	fatal("TurnOn", err)
	cfg, err = cam.GetConfig()
	fatal("GetConfig", err)
	if cfg.Format != h264.FourCC {
		fmt.Fprintln(os.Stderr, "Failed to set H264 format.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "Actual device config:", cfg2str(cfg))

	if *r {
		ctrls, err := cam.ListControls()
		fatal("ListControls", err)
		for _, ctrl := range ctrls {
			cam.SetControl(ctrl.CID, ctrl.Default)
		}
	}


	go handleInterrupt()
	//go stream(cam)


	runtime.GOMAXPROCS(runtime.NumCPU())
	
	gHub = newHub()
	go gHub.run()
	

	http.HandleFunc("/dist/", dist) 
	http.HandleFunc("/", indexHandler)  

	http.HandleFunc("/play2", func(w http.ResponseWriter, r *http.Request) {
		play2(w, r, cam)
	})
	
	fmt.Printf("wfs server lite is running....\n")
        
	http.ListenAndServe("0.0.0.0:8888", nil)
}

func cfg2str(cfg v4l.DeviceConfig) string {
	return fmt.Sprintf("%dx%d @ %.4g FPS", cfg.Width, cfg.Height,
		float64(cfg.FPS.N)/float64(cfg.FPS.D))
}


func handleInterrupt() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	log.Println("Stopping...")
	//TODO close websocket here 
	os.Exit(0)
}



func fatal(p string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
		os.Exit(1)
	}
}
