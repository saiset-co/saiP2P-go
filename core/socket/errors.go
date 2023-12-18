package socket

import (
	"errors"
)

var ErrReadTimeout = errors.New("read timeout")
