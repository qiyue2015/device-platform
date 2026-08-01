package omni

import "bytes"

type DecodedFrame struct {
	Raw   []byte
	Frame Frame
	Err   error
}

// Decoder treats TCP as a byte stream. It is bound to one explicit profile at
// listener construction time and never infers a profile from peer input.
type Decoder struct {
	profile    string
	maxBytes   int
	buffer     []byte
	discarding bool
}

func NewDecoder(profile string, maxBytes int) (*Decoder, error) {
	if !validProfile(profile) {
		return nil, newFrameError(FrameInvalidProfile)
	}
	if maxBytes < len(wireTerminator)+1 {
		return nil, newFrameError(FrameTooLarge)
	}
	return &Decoder{profile: profile, maxBytes: maxBytes, buffer: make([]byte, 0, maxBytes)}, nil
}

func (d *Decoder) Feed(chunk []byte) []DecodedFrame {
	results := make([]DecodedFrame, 0)
	terminator := []byte(wireTerminator)
	for len(chunk) > 0 {
		if d.discarding {
			index := bytes.Index(chunk, terminator)
			if index < 0 {
				return results
			}
			d.discarding = false
			chunk = chunk[index+len(terminator):]
			continue
		}

		index := bytes.Index(chunk, terminator)
		if index < 0 {
			if len(d.buffer)+len(chunk) > d.maxBytes {
				d.buffer = d.buffer[:0]
				d.discarding = true
				results = append(results, DecodedFrame{Err: newFrameError(FrameTooLarge)})
				return results
			}
			d.buffer = append(d.buffer, chunk...)
			return results
		}

		segment := chunk[:index+len(terminator)]
		if len(d.buffer)+len(segment) > d.maxBytes {
			d.buffer = d.buffer[:0]
			results = append(results, DecodedFrame{Err: newFrameError(FrameTooLarge)})
		} else {
			d.buffer = append(d.buffer, segment...)
			raw := append([]byte(nil), d.buffer...)
			frame, err := ParseFrame(d.profile, raw)
			results = append(results, DecodedFrame{Raw: raw, Frame: frame, Err: err})
			d.buffer = d.buffer[:0]
		}
		chunk = chunk[index+len(terminator):]
	}
	return results
}

func (d *Decoder) Close() []DecodedFrame {
	if d.discarding {
		d.discarding = false
		d.buffer = d.buffer[:0]
		return nil
	}
	if len(d.buffer) == 0 {
		return nil
	}
	raw := append([]byte(nil), d.buffer...)
	d.buffer = d.buffer[:0]
	return []DecodedFrame{{Raw: raw, Err: newFrameError(FrameTrailingData)}}
}
