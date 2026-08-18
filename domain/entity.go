package domain

type BayerPattern string

const (
	PatternRGGB    BayerPattern = "RGGB"
	PatternBGGR    BayerPattern = "BGGR"
	PatternGRBG    BayerPattern = "GRBG"
	PatternGBRG    BayerPattern = "GBRG"
	PatternUnknown BayerPattern = "UNKNOWN"
)

// WBFactors содержит нормализованные float32 коэффициенты баланса белого
type WBFactors struct {
	R  float32
	G1 float32
	G2 float32
	B  float32
}

type RGBImage16 struct {
	Width  int
	Height int
	Data   []uint16 // Плоский массив формата [R, G, B, R, G, B, ...]
}

// Структура метаданных
type GFXMetadata struct {
	CameraModel     string
	RAFVersion      string
	Width           uint16
	Height          uint16
	BitDepth        uint16
	IsCompressed    bool
	CompressionType uint16
	Pattern         BayerPattern
	BlackLevel      uint16
	WhiteBalance    [4]uint16 // Сырые коэффициенты из MakerNote

	// НОВОЕ ПОЛЕ: Рассчитанные и готовые к работе множители
	WBFactors WBFactors
}

// Главный заголовок RAF (84 байта)
type RAFHeader struct {
	MagicString [16]byte
	Version     [4]byte
	CameraID    [8]byte
	CameraModel [32]byte
	DirVersion  [4]byte
	Reserved    [20]byte
}

// Таблица смещений RAF (24 байта)
type RAFOffsets struct {
	JPEGOffset   uint32
	JPEGLen      uint32
	CFAHeaderOff uint32
	CFAHeaderLen uint32
	CFADataOff   uint32
	CFADataLen   uint32
}

type CFARecord struct {
	TagID uint16
	Size  uint16
}

const (
	TagGFXBayerLayout = 0x0110
	TagGFXGeometry    = 0x0111
	TagGFXBlackLevel  = 0x0200
	TagGFXBitDepth    = 0x0141
	TagGFXMakerNote   = 0xC000
)
