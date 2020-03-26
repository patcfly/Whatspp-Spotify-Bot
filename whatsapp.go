package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Rhymen/go-whatsapp"
	"github.com/zmb3/spotify"
)

const redirectURI = "http://localhost:8080/callback"
const playlistId = "0s3a3Wd7DNsg3yisDdSo8k"

var (
	auth  = spotify.NewAuthenticator(redirectURI, spotify.ScopePlaylistModifyPublic)
	ch    = make(chan *spotify.Client)
	state = "abc123"
	sema = make(chan struct{},1)
	allTracks = make(map[string]bool)
)

type waHandler struct {
	c *whatsapp.Conn
	s *spotify.Client
}

//HandleError needs to be implemented to be a valid WhatsApp handler
func (h *waHandler) HandleError(err error) {
	if e, ok := err.(*whatsapp.ErrConnectionFailed); ok {
		log.Printf("Connection failed, underlying error: %v", e.Err)
		log.Println("Waiting 30sec...")
		<-time.After(30 * time.Second)
		log.Println("Reconnecting...")
		err := h.c.Restore()
		if err != nil {
			log.Fatalf("Restore failed: %v", err)
		}
	} else {
		log.Printf("error occoured: %v\n", err)
	}
}

func (h *waHandler) HandleTextMessage(message whatsapp.TextMessage) {
	splitData := strings.FieldsFunc(message.Info.RemoteJid, Split)
	if splitData[1] == "1500829488" {
		r, _ := regexp.Compile("http[s]?://(?:[a-zA-Z]|[0-9]|[$-_@.&+]|[!*(),]|(?:%[0-9a-fA-F][0-9a-fA-F]))+")
		urls := r.FindAllString(message.Text,-1)
		for i := range urls {
			urlObject , err := url.Parse(urls[i])
			if err != nil {
				panic(err)
			}
			if urlObject.Host=="open.spotify.com"{
				path := strings.Split(urlObject.Path,"/")
				if len(path)>2 && path[1]=="track" {
					track, err := h.s.GetTrack(spotify.ID(path[2]))
					if err!=nil{
						//log.Fatal(err)
						fmt.Printf("couldnt find song error : ",err)
						return
					}
					fmt.Printf(track.Name + "  " + track.Album.Name + "\n")
					sema<- struct{}{}
					exists := allTracks[track.ID.String()]
					if !exists {
						//add to the playlist
						_, err := h.s.AddTracksToPlaylist(playlistId,spotify.ID(path[2]))
						if err != nil {
							log.Fatal(err)
						}else{
							allTracks[path[2]] = true
						}
					}
					//release
					<-sema
				}
			}
		}
	}
}

func Split(r rune) bool {
	return r == '-' || r == '@'
}

func main() {
	//get spotify creds
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(":8080", nil)

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	// use the client to make calls that require authorization
	user, err := client.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}
	playlist,error := client.GetPlaylist(playlistId)
	if error!=nil{
		log.Fatal(error)
	}
	fmt.Println("You are logged in as:", user.ID," Playlist : ",playlist.Name)
	getAllTracks(client,playlist.ID.String(),playlist.Name)

	for key, _ := range allTracks {
		fmt.Printf(key)
	}

	//create new WhatsApp connection
	wac, err := whatsapp.NewConn(5 * time.Second)
	if err != nil {
		log.Fatalf("error creating connection: %v\n", err)
	}

	//Add handler
	wac.AddHandler(&waHandler{wac,client})

	//login or restore
	if err := login(wac); err != nil {
		log.Fatalf("error logging in: %v\n", err)
	}

	for{
		//verifies phone connectivity
		pong, err := wac.AdminTest()

		if !pong || err != nil {
			//log.Fatalf("error pinging in: %v\n", err)
			fmt.Printf("error pinging in: %v\n", err)
		}else{
			break
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	//Disconnect safe
	fmt.Println("Shutting down now.")
	session, err := wac.Disconnect()
	if err != nil {
		log.Fatalf("error disconnecting: %v\n", err)
	}
	if err := writeSession(session); err != nil {
		log.Fatalf("error saving session: %v", err)
	}
}

func login(wac *whatsapp.Conn) error {
	//load saved session
	session, err := readSession()
	if err == nil {
		//restore session
		session, err = wac.RestoreWithSession(session)
		if err != nil {
			return fmt.Errorf("restoring failed: %v\n", err)
		}
	} else {
		//no saved session -> regular login
		qr := make(chan string)
		go func() {
			terminal := qrcodeTerminal.New()
			terminal.Get(<-qr).Print()
		}()
		wac.SetClientVersion(0, 4, 1307)
		session, err = wac.Login(qr)
		if err != nil {
			return fmt.Errorf("error during login: %v\n", err)
		}
	}

	//save session
	err = writeSession(session)
	if err != nil {
		return fmt.Errorf("error saving session: %v\n", err)
	}
	return nil
}

func readSession() (whatsapp.Session, error) {
	session := whatsapp.Session{}
	file, err := os.Open(os.TempDir() + "/whatsappSession.gob")
	if err != nil {
		return session, err
	}
	defer file.Close()
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&session)
	if err != nil {
		return session, err
	}
	return session, nil
}

func writeSession(session whatsapp.Session) error {
	file, err := os.Create(os.TempDir() + "/whatsappSession.gob")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(session)
	if err != nil {
		return err
	}
	return nil
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	// use the token to get an authenticated client
	client := auth.NewClient(tok)
	_, _ = fmt.Fprintf(w, "Login Completed!")
	ch <- &client
}

func getAllTracks(client *spotify.Client, playlistId string,playlistName string){
	sema <- struct{}{}
	limit := 100
	offset := 0

	var options spotify.Options
	options.Limit = &limit
	options.Offset = &offset

	for {
		tracksPage, err := client.GetPlaylistTracksOpt(spotify.ID(playlistId), &options, "")
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Playlist %v: fetched page of %v track(s)", playlistName, len(tracksPage.Tracks))

		for _, playlistTrack := range tracksPage.Tracks {
			track := playlistTrack.Track
			allTracks[track.ID.String()] = true
		}

		// The Spotify API always returns full pages unless it has none left to
		// return.
		if len(tracksPage.Tracks) < 100 {
			break
		}

		offset = offset + len(tracksPage.Tracks)
	}
	<- sema
	log.Printf("Playlist %v: %v track(s)", playlistName, len(allTracks))
}

