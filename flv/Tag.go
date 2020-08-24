package flv

type Flv struct {
	// 文件头信息
	Header Header
	Body   Body
}

type Header struct {
	Signature  uint32 //3byte
	Version    uint8
	Flags      uint8
	DataOffset uint32
}

type Body struct {
	blocks []Block
}

type Block struct {
	//上一个 Tag 字节大小
	PreviousTagSize uint32
	Tag             Tag
}

type Tag struct {
	header     TagHeader
	bodyHeader TagBodyHeader
	body       []byte
}

type TagHeader struct {
	TagType           uint8  // 1byte  8为Audio,9为Video,18为scripts
	TagDataSize       uint32 // 3byte
	Timestamps        uint32 // 3byte
	TimestampExtended uint32 //3byte
	StreamId          uint32 // 3byte
}

type TagBodyHeader struct {
}

//音频内容头
type AudioTagBodyHeader struct {
	TagBodyHeader

	//音频格式
	AudioType uint8 // 4bit

	//采样率
	SamplingRate uint8 // 2bit

	// 采样长度
	SamplingLength uint8 // 1bit

	//音频类型
	AudioMode uint8 // 1bit
}

func (this AudioTagBodyHeader) toByte() byte {
	u := this.AudioType<<4 | this.SamplingRate<<2 | this.SamplingLength<<1 | this.AudioMode
	return byte(u)
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
