package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/fireflyhoo/live-spirit/signal"
	"github.com/pion/rtcp"
	_ "github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
	"github.com/pion/webrtc/v2/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v2/pkg/media/oggwriter"
	"github.com/pion/webrtc/v2/pkg/media/h264writer"
	"github.com/q191201771/lal/pkg/rtmp"
)

func main() {
	http.HandleFunc("/index", indexHandler)
	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/getOfferAnswer", getOfferAnswer)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	log.Printf("Open http://localhost:%s in the browser", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

func getOfferAnswer(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var offer = r.PostForm.Get("offer")
	var answer = getWebrtcOfferAnswer(offer)
	w.Write([]byte("{" +
		"\"answer\":" + "\"" + answer + "\"" +
		"}"))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, err := fmt.Fprint(w, "Hello, World!")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func getWebrtcOfferAnswer(offers string) string {
	m := webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.NewRTPOpusCodec(webrtc.DefaultPayloadTypeOpus, 48000))
	m.RegisterCodec(webrtc.NewRTPH264Codec(webrtc.DefaultPayloadTypeH264, 90000))
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.xten.com"},
			},
		},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	} else if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	rand.Seed(time.Now().UnixNano())
	i := rand.Intn(1000000)
	oggFile, err := oggwriter.New("./yy"+strconv.Itoa(i)+"output.ogg", 48000, 2)
	if err != nil {
		panic(err)
	}
	ivfFile, err := ivfwriter.New("./yy" + strconv.Itoa(i) + "output.ivf")
	if err != nil {
		panic(err)
	}
	h246File, err  := h264writer.New("./yy" + strconv.Itoa(i) + "h246.flv")

	//webrtc 每一道流都会触发一次回调
	peerConnection.OnTrack(func(track *webrtc.Track, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: track.SSRC()}})
				if errSend != nil {
					fmt.Println(errSend)
				}
			}
		}()
		fmt.Println("开始考虑推流的问题")

		codec := track.Codec()
		if codec.Name == webrtc.Opus {
			fmt.Println("Got Opus track, saving to disk as output.opus (48 kHz, 2 channels)")
			saveToDisk(oggFile, track)
		} else if codec.Name == webrtc.VP8 {
			fmt.Println("Got VP8 track, saving to disk as output.ivf")
			saveToDisk(ivfFile, track)
		} else if codec.Name == webrtc.H264 {
			fmt.Println("H264:--->")
			//sendH264ToRtmp(track)
			saveToDisk(h246File,track)
		}
	})

	// 连接状态回调
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateConnected {
			fmt.Println("Ctrl+C the remote client to stop the demo")
		} else if connectionState == webrtc.ICEConnectionStateFailed ||
			connectionState == webrtc.ICEConnectionStateDisconnected {
			closeErr := oggFile.Close()
			if closeErr != nil {
				panic(closeErr)
			}
			closeErr = ivfFile.Close()
			if closeErr != nil {
				panic(closeErr)
			}
			fmt.Println("Done writing media files")
		}
	})

	offer := webrtc.SessionDescription{}
	signal.Decode(offers, &offer)

	// Set the remote SessionDescription
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		panic(err)
	}

	// Output the answer in base64 so we can paste it in browser
	return signal.Encode(answer)
}

func sendH264ToRtmp(track *webrtc.Track) {
	fmt.Println("推流试试...")
	var url = "rtmp://127.0.0.1:1935/live/live-js"

	ps := rtmp.NewPushSession(func(option *rtmp.PushSessionOption) {
		option.ConnectTimeoutMS = 3000
		option.PushTimeoutMS = 5000
		option.WriteAVTimeoutMS = 10000
	})
	err := ps.Push(url)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		rtpPacket, err := track.ReadRTP()
		if err != nil {
			panic(err)
			return
		}
		println(rtpPacket)
		//h264writer.WriteRTP(rtpPacket)
	}
}

func saveToDisk(i media.Writer, track *webrtc.Track) {
	defer func() {
		if err := i.Close(); err != nil {
			panic(err)
		}
	}()

	for {
		rtpPacket, err := track.ReadRTP()
		if err != nil {
			panic(err)
		}
		if err := i.WriteRTP(rtpPacket); err != nil {
			panic(err)
		}
	}
}
