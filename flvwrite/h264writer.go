package flvwrite

import (
	"github.com/pion/rtp"
	"github.com/q191201771/lal/pkg/rtmp"
	//"github.com/pion/rtp/codecs"
)

type H264Writer struct {
	count        uint64
	currentFrame []byte
	PushSessoin  *rtmp.PushSession
	bufferPacket [100]*rtp.Packet
}


func (i *H264Writer) Close() error {
	return nil
}
