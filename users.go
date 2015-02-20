package gojira

import (
	"fmt"
	"encoding/json"
)

const (
	user_url        = "/user"
)

type User struct {
	Self         string            `json:"self"`
	Name         string            `json:"name"`
	EmailAddress string            `json:"emailAddress"`
	DisplayName  string            `json:"displayName"`
	Active       bool              `json:"active"`
	TimeZone     string            `json:"timeZone"`
	AvatarUrls   map[string]string `json:"avatarUrls"`
	Expand       string            `json:"expand"`
}

func (j *Jira) User(username string) (*User, error) {
	url := j.BaseUrl + j.ApiPath + user_url + "?username=" + username
	contents, _ := j.buildAndExecRequest("GET", url)

	user := new(User)
	err := json.Unmarshal(contents, &user)
	if err != nil {
		fmt.Println("%s", err)
	}

	return user, err
}
