package kefw2

import (
	"encoding/json"
	"fmt"
)

type PlayerData struct {
	State      string           `json:"state"`
	Status     PlayerResource   `json:"status"`
	TrackRoles PlayerTrackRoles `json:"trackRoles"`
	Controls   PlayerControls   `json:"controls"`
	MediaRoles PlayerMediaRoles `json:"mediaRoles"`
	PlayID     PlayerPlayID     `json:"playId"`
}

type PlayerResource struct {
	Duration int `json:"duration"`
}

type PlayerTrackRoles struct {
	Icon      string          `json:"icon"`
	MediaData PlayerMediaData `json:"mediaData"`
	Title     string          `json:"title"`
}

type PlayerMediaData struct {
	ActiveResource PlayerResource   `json:"activeResource"`
	MetaData       PlayerMetaData   `json:"metaData"`
	Resources      []PlayerResource `json:"resources"`
}

type PlayerMetaData struct {
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

type PlayerControls struct {
	Previous bool `json:"previous"`
	Pause    bool `json:"pause"`
	Next     bool `json:"next_"`
}

type PlayerMediaRoles struct {
	AudioType  string                   `json:"audioType"`
	DoNotTrack bool                     `json:"doNotTrack"`
	Type       string                   `json:"type"`
	MediaData  PlayerMediaRolesMetaData `json:"mediaData"`
	Title      string                   `json:"title"`
}

type PlayerMediaRolesMetaData struct {
	MetaData  PlayerMediaRolesMedieDataMetaData `json:"metaData"`
	Resources []PlayerMimeResource              `json:"resources"`
}

type PlayerMediaRolesMedieDataMetaData struct {
	ServiceID     string `json:"serviceId"`
	Live          bool   `json:"live"`
	PlayLogicPath string `json:"playLogicPath"`
}

type PlayerMimeResource struct {
	MimeType string `json:"mimeType"`
	URI      string `json:"uri"`
}

type PlayerPlayID struct {
	TimeStamp      int    `json:"timestamp"`
	SystemMemberId string `json:"systemMemberId"`
}

func (s *KEFSpeaker) PlayerData() (PlayerData, error) {
	var playersData []PlayerData
	var err error
	playersJson, err := s.getData("player:player/data")
	if err != nil {
		return PlayerData{}, fmt.Errorf("error getting player data: %s", err)
	}
	err = json.Unmarshal(playersJson, &playersData)
	if err != nil {
		// fmt.Printf("jsonData: %+v\n", string(playersJson))
		return PlayerData{}, fmt.Errorf("error unmarshaling player data: %s", err)
	}
	playerData := playersData[0]
	return playerData, nil
}

// String returns the duration in minutes:seconds format instead of milliseconds
func (p PlayerResource) String() string {
	inSeconds := p.Duration / 1000
	minutes := inSeconds / 60
	seconds := inSeconds % 60
	str := fmt.Sprintf("%d:%02d", minutes, seconds)
	return str
}
