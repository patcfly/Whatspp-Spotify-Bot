package main
import (
	"fmt"
	"github.com/Rhymen/go-whatsapp"
	"time"
)




func main() {
	wac, _ := whatsapp.NewConn(20 * time.Second)
	qrChan := make(chan string)
	go func() {
		b64LoginToken := <-qrChan
		fmt.Printf("qr code: %v\n",b64LoginToken)
		//show qr code or save it somewhere to scan
	}()
	wac.Login(qrChan)
}

