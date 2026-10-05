package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

// Call runs one API method on the socket at path and writes the result as
// JSON to out. For "subscribe" it writes events, one per line, until the
// connection ends. params is a JSON object, or "".
func Call(path, method, params string, out io.Writer) error {
	c, err := net.Dial("unix", path)
	if err != nil {
		return errors.New("whatsapp-tui isn't running (start it first)")
	}
	defer c.Close()
	req := map[string]any{"id": 1, "method": method}
	if strings.TrimSpace(params) != "" {
		req["params"] = json.RawMessage(params)
		if !json.Valid([]byte(params)) {
			return fmt.Errorf("params aren't JSON: %s", params)
		}
	}
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return err
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var resp Response
		if err := json.Unmarshal(sc.Bytes(), &resp); err == nil && resp.ID != nil {
			if resp.Error != "" {
				return errors.New(resp.Error)
			}
			if method != "subscribe" {
				b, _ := json.MarshalIndent(resp.Result, "", "  ")
				_, err := fmt.Fprintln(out, string(b))
				return err
			}
			continue // "subscribed"; events follow
		}
		if _, err := fmt.Fprintln(out, sc.Text()); err != nil { // an event
			return err
		}
	}
	return sc.Err()
}
