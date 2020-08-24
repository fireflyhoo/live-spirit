package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/nareix/bits"
	"github.com/pion/rtp"
	"github.com/q191201771/lal/pkg/httpflv"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
	"io"

	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/fireflyhoo/live-spirit/flv"
	_ "github.com/fireflyhoo/live-spirit/h264/parser"
	"github.com/fireflyhoo/live-spirit/signal"
	"github.com/pion/rtcp"
	_ "github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
	"github.com/pion/webrtc/v2/pkg/media/h264writer"
	"github.com/pion/webrtc/v2/pkg/media/oggwriter"
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

	h246File, err := h264writer.New("./yy" + strconv.Itoa(i) + "h246.264")
	if err != nil {
		panic(err)
	}

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
		} else if codec.Name == webrtc.H264 {
			fmt.Println("H264:--->")
			//sendH264ToRtmp(track)
			go saveToDisk(h246File, track)
			//fmt.Print("发送到 rtmp 试试")
			//go sendToRtmp(track)
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
			closeErr = h246File.Close()
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

func sendToRtmp(track *webrtc.Track) error {
	defer func() {

	}()
	hasKeyFrame := false
	var url = "rtmp://127.0.0.1:1935/live/live-js"

	ps := rtmp.NewPushSession(func(option *rtmp.PushSessionOption) {
		option.ConnectTimeoutMS = 3000
		option.PushTimeoutMS = 5000
		option.WriteAVTimeoutMS = 10000
	})
	err := ps.Push(url)
	if err != nil {
		fmt.Println(err)
		return err
	}

	tags := readAllTag("dome.flv")
	fast := true
	for {

		packet, err := track.ReadRTP()
		for _, tag := range tags {
			if tag.IsMetadata() && fast {
				h := logic.Trans.FLVTagHeader2RTMPHeader(tag.Header)
				h.Timestamp = packet.Timestamp
				h.TimestampAbs = 0
				chunks := rtmp.Message2Chunks(tag.Raw[11:11+h.MsgLen], &h)
				ps.AsyncWrite(chunks)
				fast = false
			}
		}

		if len(packet.Payload) == 0 {
			continue
		}

		if !hasKeyFrame {
			if hasKeyFrame = isKeyFrame(packet.Payload); !hasKeyFrame {
				// key frame not defined yet. discarding packet
				continue
			}
		}
		//h264 裸流
		data := packet.Payload
		fmt.Println("一帧数据")
		v := VideoTagBodyHeader{}
		v.FrameType = 1
		v.CoderId = 7 // h264

		types, e := ParseSliceHeaderFromNALU(data)
		fmt.Println(types, e)

		data = append([]byte{v.toByte()}, data...)

		header := rtpVideoHeaer2RTMPHeader(packet.Header, uint32(len(data)))
		chunks := rtmp.Message2Chunks(data, &header)
		sendRtmpChunks(chunks, ps)
		if err != nil {
			return err
		}
	}

}

func sendRtmpChunks(chunks []byte, ps *rtmp.PushSession) {
	if err := ps.AsyncWrite(chunks); err != nil {
		nazalog.Errorf("write data error. err=%v", err)
		return
	}
}

func rtpVideoHeaer2RTMPHeader(header rtp.Header, leng uint32) (out rtmp.Header) {
	out.MsgLen = leng
	out.MsgTypeID = 9 //Video TagTypeAudio
	out.MsgStreamID = rtmp.MSID1
	out.CSID = rtmp.CSIDVideo
	out.Timestamp = header.Timestamp
	out.TimestampAbs = header.Timestamp
	return
}

// readAllTag 预读取 flv 文件中的所有 tag，缓存在内存中
func readAllTag(filename string) (ret []httpflv.Tag) {
	var ffr httpflv.FLVFileReader
	err := ffr.Open(filename)
	fmt.Println(err)
	fmt.Println("open succ. filename=%s", filename)

	for {
		tag, err := ffr.ReadTag()
		if err == io.EOF {
			fmt.Println("EOF")
			break
		}
		fmt.Println(err)
		ret = append(ret, tag)
	}
	fmt.Println("read all tag done. num=%d", len(ret))
	return
}

func FLVTagHeader2RTMPHeader(in httpflv.TagHeader) (out rtmp.Header) {
	out.MsgLen = in.DataSize
	out.MsgTypeID = in.Type
	out.MsgStreamID = rtmp.MSID1
	switch in.Type {
	case httpflv.TagTypeMetadata:
		out.CSID = rtmp.CSIDAMF
	case httpflv.TagTypeAudio:
		out.CSID = rtmp.CSIDAudio
	case httpflv.TagTypeVideo:
		out.CSID = rtmp.CSIDVideo
	}
	out.Timestamp = in.Timestamp
	out.TimestampAbs = in.Timestamp
	return
}

func isKeyFrame(data []byte) bool {
	const typeSTAPA = 24

	var word uint32

	payload := bytes.NewReader(data)
	err := binary.Read(payload, binary.BigEndian, &word)

	if err != nil || (word&0x1F000000)>>24 != typeSTAPA {
		return false
	}

	return word&0x1F == 7
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

//视频内容头
type VideoTagBodyHeader struct {
	//帧类型
	FrameType uint8 // 4bit

	//编码id
	CoderId uint8 // 4bit
}

func (this VideoTagBodyHeader) toByte() byte {
	u := (this.FrameType << 4) | this.CoderId
	return byte(u)
}

func ParseSliceHeaderFromNALU(packet []byte) (nalu_type string, err error) {

	if len(packet) <= 1 {
		err = fmt.Errorf("%s", "H264Parser.Packet.Too.Short.To.Parse.Slice.Header")
		return
	}

	nal_unit_type := packet[0] & 0x1f
	switch nal_unit_type {
	case 1, 2, 5, 19:

	// slice_layer_without_partitioning_rbsp
	// slice_data_partition_a_layer_rbsp

	default:
		err = fmt.Errorf("h264parser.nal_unit_type=%d Has.No.Slice.Header", nal_unit_type)
		return
	}

	r := &bits.GolombBitReader{R: bytes.NewReader(packet[1:])}

	// first_mb_in_slice
	if _, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	// slice_type
	var u uint
	if u, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	var nalu_typei SliceType
	switch u {
	case 0, 3, 5, 8:
		nalu_typei = SLICE_P
		nalu_type = nalu_typei.String()
	case 1, 6:
		nalu_typei = SLICE_B
		nalu_type = nalu_typei.String()
	case 2, 4, 7, 9:
		nalu_typei = SLICE_I
		nalu_type = nalu_typei.String()
	default:
		err = fmt.Errorf("H264Parser.Slice_type=%d.Invalid", u)
		return
	}

	return
}

type SliceType uint

func (self SliceType) String() string {
	switch self {
	case SLICE_P:
		return "P"
	case SLICE_B:
		return "B"
	case SLICE_I:
		return "I"
	}
	return ""
}

const (
	SLICE_P = iota + 1
	SLICE_B
	SLICE_I
)
