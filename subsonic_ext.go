package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type ArtistInfo struct {
	Biography     string   `json:"biography"`
	LastFmURL     string   `json:"lastFmUrl"`
	SimilarArtist []Artist `json:"similarArtist"`
}

type AlbumInfo struct {
	Notes     string `json:"notes"`
	LastFmURL string `json:"lastFmUrl"`
}

type albumList struct {
	Album []Album `json:"album"`
}

type songList struct {
	Song []Song `json:"song"`
}

// call performs a request and decodes the response, turning Subsonic
// errors into Go errors.
func (c *SubsonicClient) call(path string, params url.Values) (*subsonicResponse, error) {
	data, err := c.get(path, params)
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if e := resp.SubsonicResponse.Error; e != nil {
		return nil, fmt.Errorf("subsonic error: %s", e.Message)
	}
	return &resp, nil
}

// Scrobble reports a song as now playing (submission false) or as played
// at the given time (submission true).
func (c *SubsonicClient) Scrobble(id string, at time.Time, submission bool) error {
	_, err := c.call("scrobble", url.Values{
		"id":         {id},
		"time":       {strconv.FormatInt(at.UnixMilli(), 10)},
		"submission": {strconv.FormatBool(submission)},
	})
	return err
}

func (c *SubsonicClient) GetArtistInfo(id string) (*ArtistInfo, error) {
	resp, err := c.call("getArtistInfo2", url.Values{"id": {id}, "count": {"10"}})
	if err != nil {
		return nil, err
	}
	if resp.SubsonicResponse.ArtistInfo2 == nil {
		return &ArtistInfo{}, nil
	}
	return resp.SubsonicResponse.ArtistInfo2, nil
}

func (c *SubsonicClient) GetAlbumInfo(id string) (*AlbumInfo, error) {
	resp, err := c.call("getAlbumInfo2", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	if resp.SubsonicResponse.AlbumInfo == nil {
		return &AlbumInfo{}, nil
	}
	return resp.SubsonicResponse.AlbumInfo, nil
}

// GetAlbumList returns albums of the given list type, such as "newest",
// "recent", "frequent", "highest", "random" or "starred".
func (c *SubsonicClient) GetAlbumList(listType string, size int) ([]Album, error) {
	resp, err := c.call("getAlbumList2", url.Values{"type": {listType}, "size": {strconv.Itoa(size)}})
	if err != nil {
		return nil, err
	}
	if resp.SubsonicResponse.AlbumList2 == nil {
		return nil, nil
	}
	return resp.SubsonicResponse.AlbumList2.Album, nil
}

func (c *SubsonicClient) GetRandomSongs(size int) ([]Song, error) {
	resp, err := c.call("getRandomSongs", url.Values{"size": {strconv.Itoa(size)}})
	if err != nil {
		return nil, err
	}
	if resp.SubsonicResponse.RandomSongs == nil {
		return nil, nil
	}
	return resp.SubsonicResponse.RandomSongs.Song, nil
}

func (c *SubsonicClient) GetStarredSongs() ([]Song, error) {
	resp, err := c.call("getStarred2", nil)
	if err != nil {
		return nil, err
	}
	if resp.SubsonicResponse.Starred2 == nil {
		return nil, nil
	}
	return resp.SubsonicResponse.Starred2.Song, nil
}

// starParam is the parameter name star/unstar use for each kind of item.
var starParam = map[itemKind]string{
	itemSong:   "id",
	itemAlbum:  "albumId",
	itemArtist: "artistId",
}

// SetStarred stars or unstars an item.
func (c *SubsonicClient) SetStarred(kind itemKind, id string, star bool) error {
	path := "unstar"
	if star {
		path = "star"
	}
	_, err := c.call(path, url.Values{starParam[kind]: {id}})
	return err
}

// SetRating sets the rating (1-5) of an item; 0 removes the rating.
func (c *SubsonicClient) SetRating(id string, rating int) error {
	_, err := c.call("setRating", url.Values{"id": {id}, "rating": {strconv.Itoa(rating)}})
	return err
}
