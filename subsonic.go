package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
)

type SubsonicClient struct {
	BaseURL  string
	User     string
	Password string
	client   *http.Client
}

func NewSubsonicClient(baseURL, user, password string) *SubsonicClient {
	return &SubsonicClient{
		BaseURL:  baseURL,
		User:     user,
		Password: password,
		client:   &http.Client{},
	}
}

func (c *SubsonicClient) authParams() url.Values {
	salt := fmt.Sprintf("%06d", rand.Intn(1000000))
	token := fmt.Sprintf("%x", md5.Sum([]byte(c.Password+salt)))
	return url.Values{
		"u": {c.User},
		"t": {token},
		"s": {salt},
		"v": {"1.16.1"},
		"c": {"nd"},
		"f": {"json"},
	}
}

func (c *SubsonicClient) get(path string, extra url.Values) ([]byte, error) {
	params := c.authParams()
	for k, v := range extra {
		params[k] = v
	}
	u := c.BaseURL + "/rest/" + path + "?" + params.Encode()
	resp, err := c.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *SubsonicClient) StreamURL(id string) string {
	params := c.authParams()
	params.Set("id", id)
	return c.BaseURL + "/rest/stream?" + params.Encode()
}

// Subsonic API response types

type subsonicResponse struct {
	SubsonicResponse struct {
		Status        string         `json:"status"`
		Error         *subsonicError `json:"error"`
		Artists       *artistIndex   `json:"artists"`
		Artist        *artistDetail  `json:"artist"`
		Album         *albumDetail   `json:"album"`
		SearchResult3 *searchResult  `json:"searchResult3"`
		Playlists     *playlists     `json:"playlists"`
		Playlist      *playlist      `json:"playlist"`
		LyricsList    *lyricsList    `json:"lyricsList"`
	} `json:"subsonic-response"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type artistIndex struct {
	Index []struct {
		Name   string   `json:"name"`
		Artist []Artist `json:"artist"`
	} `json:"index"`
}

type artistDetail struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Album []Album `json:"album"`
}

type albumDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Song []Song `json:"song"`
}

type searchResult struct {
	Artist []Artist `json:"artist"`
	Album  []Album  `json:"album"`
	Song   []Song   `json:"song"`
}

type playlists struct {
	Playlist []Playlist `json:"playlist"`
}

type playlist struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Entry []Song `json:"entry"`
}

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount"`
}

type Album struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artistId"`
	SongCount int    `json:"songCount"`
	Year      int    `json:"year"`
}

type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Album    string `json:"album"`
	Artist   string `json:"artist"`
	Duration int    `json:"duration"`
	Track    int    `json:"track"`
}

type Playlist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"songCount"`
}

type lyricsList struct {
	StructuredLyrics []StructuredLyrics `json:"structuredLyrics"`
}

type StructuredLyrics struct {
	Lang   string      `json:"lang"`
	Synced bool        `json:"synced"`
	Line   []LyricLine `json:"line"`
}

type LyricLine struct {
	Start *int   `json:"start"` // milliseconds, nil for unsynced
	Value string `json:"value"`
}

func (c *SubsonicClient) GetArtists() ([]Artist, error) {
	data, err := c.get("getArtists", nil)
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.Artists == nil {
		return nil, nil
	}
	var artists []Artist
	for _, idx := range r.Artists.Index {
		artists = append(artists, idx.Artist...)
	}
	return artists, nil
}

func (c *SubsonicClient) GetArtist(id string) ([]Album, error) {
	data, err := c.get("getArtist", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.Artist == nil {
		return nil, nil
	}
	return r.Artist.Album, nil
}

func (c *SubsonicClient) GetAlbum(id string) ([]Song, error) {
	data, err := c.get("getAlbum", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.Album == nil {
		return nil, nil
	}
	return r.Album.Song, nil
}

func (c *SubsonicClient) Search(query string) (*searchResult, error) {
	data, err := c.get("search3", url.Values{"query": {query}})
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	return r.SearchResult3, nil
}

func (c *SubsonicClient) GetPlaylists() ([]Playlist, error) {
	data, err := c.get("getPlaylists", nil)
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.Playlists == nil {
		return nil, nil
	}
	return r.Playlists.Playlist, nil
}

func (c *SubsonicClient) GetPlaylist(id string) ([]Song, error) {
	data, err := c.get("getPlaylist", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.Playlist == nil {
		return nil, nil
	}
	return r.Playlist.Entry, nil
}

func (c *SubsonicClient) GetLyrics(songID string) ([]StructuredLyrics, error) {
	data, err := c.get("getLyricsBySongId", url.Values{"id": {songID}})
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	r := resp.SubsonicResponse
	if r.Error != nil {
		return nil, fmt.Errorf("subsonic error: %s", r.Error.Message)
	}
	if r.LyricsList == nil {
		return nil, nil
	}
	return r.LyricsList.StructuredLyrics, nil
}

func (c *SubsonicClient) Ping() error {
	data, err := c.get("ping", nil)
	if err != nil {
		return err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	if resp.SubsonicResponse.Error != nil {
		return fmt.Errorf("subsonic error: %s", resp.SubsonicResponse.Error.Message)
	}
	if resp.SubsonicResponse.Status != "ok" {
		return fmt.Errorf("unexpected status: %s", resp.SubsonicResponse.Status)
	}
	return nil
}
