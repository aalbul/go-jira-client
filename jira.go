package gojira

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"math"
	"io"
	"bytes"
	"mime/multipart"
	"path/filepath"
	"os"
	"log"
	"strings"
)

type JiraError struct {
	What string
}

func (e JiraError) Error() string {
	return e.What
}

type Jira struct {
	BaseUrl      string
	ApiPath      string
	ActivityPath string
	Client       *http.Client
	Auth         *Auth
}

type Auth struct {
	Login    string
	Password string
}

type Pagination struct {
	Total      int
	StartAt    int
	MaxResults int
	Page       int
	PageCount  int
	Pages      []int
}

func (p *Pagination) Compute() {
	p.PageCount = int(math.Ceil(float64(p.Total) / float64(p.MaxResults)))
	p.Page = int(math.Ceil(float64(p.StartAt) / float64(p.MaxResults)))

	p.Pages = make([]int, p.PageCount)
	for i := range p.Pages {
		p.Pages[i] = i
	}
}

type Issue struct {
	Id        string
	Key       string
	Self      string
	Expand    string
	Fields    *IssueFields
	CreatedAt time.Time
}

type IssueList struct {
	Expand     string
	StartAt    int
	MaxResults int
	Total      int
	Issues     []*Issue
	Pagination *Pagination
}

type Attachment struct {
	Self        string    `json:"self"`
	Id          string    `json:"id"`
	Filename    string    `json:"filename"`
	Author      Person    `xml:"author"json:"author"`
	Size        int        `json:"size"`
	MimeType    string    `json:"mimetype"`
	Content     string    `json:"content"`
}

type IssueFields struct {
	IssueType   *IssueType
	Summary     string
	Description string
	Reporter    *User
	Assignee    *User
	Project     *JiraProject
	Attachment  []Attachment
	Created     string
}

type IssueType struct {
	Self        string
	Id          string
	Description string
	IconUrl     string
	Name        string
	Subtask     bool
}

type JiraProject struct {
	Self       string
	Id         string
	Key        string
	Name       string
	AvatarUrls map[string]string
}

type ActivityItem struct {
	Title    string    `xml:"title"json:"title"`
	Id       string    `xml:"id"json:"id"`
	Link     []Link    `xml:"link"json:"link"`
	Updated  time.Time `xml:"updated"json:"updated"`
	Author   Person    `xml:"author"json:"author"`
	Summary  Text      `xml:"summary"json:"summary"`
	Category Category  `xml:"category"json:"category"`
}

type ActivityFeed struct {
	XMLName  xml.Name        `xml:"http://www.w3.org/2005/Atom feed"json:"xml_name"`
	Title    string          `xml:"title"json:"title"`
	Id       string          `xml:"id"json:"id"`
	Link     []Link          `xml:"link"json:"link"`
	Updated  time.Time       `xml:"updated,attr"json:"updated"`
	Author   Person          `xml:"author"json:"author"`
	Entries  []*ActivityItem `xml:"entry"json:"entries"`
}

type Category struct {
	Term string `xml:"term,attr"json:"term"`
}

type Link struct {
	Rel  string `xml:"rel,attr,omitempty"json:"rel"`
	Href string `xml:"href,attr"json:"href"`
}

type Person struct {
	Name     string `xml:"name"json:"name"`
	URI      string `xml:"uri"json:"uri"`
	Email    string `xml:"email"json:"email"`
	InnerXML string `xml:",innerxml"json:"inner_xml"`
}

type Text struct {
	Type string `xml:"type,attr,omitempty"json:"type"`
	Body string `xml:",chardata"json:"body"`
}

func NewJira(baseUrl string, apiPath string, activityPath string, auth *Auth) *Jira {

	client := &http.Client{}

	return &Jira{
		BaseUrl:      baseUrl,
		ApiPath:      apiPath,
		ActivityPath: activityPath,
		Client:       client,
		Auth:         auth,
	}
}

const (
	dateLayout = "2006-01-02T15:04:05.000-0700"
)

func (j *Jira) buildAndExecRequest(method string, url string) ([]byte, int) {

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		panic("Error while building jira request")
	}
	req.SetBasicAuth(j.Auth.Login, j.Auth.Password)

	resp, err := j.Client.Do(req)
	defer resp.Body.Close()
	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("%s", err)
	}

	return contents, resp.StatusCode
}

func (j *Jira) UserActivity(user string) (ActivityFeed, error) {
	url := j.BaseUrl + j.ActivityPath + "?streams=" + url.QueryEscape("user IS " + user)

	return j.Activity(url)
}

func (j *Jira) Activity(url string) (ActivityFeed, error) {

	contents, _ := j.buildAndExecRequest("GET", url)

	var activity ActivityFeed
	err := xml.Unmarshal(contents, &activity)
	if err != nil {
		fmt.Println("%s", err)
	}

	return activity, err
}

// search issues assigned to given user
func (j *Jira) IssuesAssignedTo(user string, maxResults int, startAt int) IssueList {

	url := j.BaseUrl + j.ApiPath + "/search?jql=assignee=\"" + url.QueryEscape(user) + "\"&startAt=" + strconv.Itoa(startAt) + "&maxResults=" + strconv.Itoa(maxResults)
	contents, _ := j.buildAndExecRequest("GET", url)

	var issues IssueList
	err := json.Unmarshal(contents, &issues)
	if err != nil {
		fmt.Println("%s", err)
	}

	for _, issue := range issues.Issues {
		t, _ := time.Parse(dateLayout, issue.Fields.Created)
		issue.CreatedAt = t
	}

	pagination := Pagination{
		Total:      issues.Total,
		StartAt:    issues.StartAt,
		MaxResults: issues.MaxResults,
	}
	pagination.Compute()

	issues.Pagination = &pagination

	return issues
}

// search an issue by its id
func (j *Jira) Issue(id string) (Issue, error) {

	url := j.BaseUrl + j.ApiPath + "/issue/" + id
	contents, code := j.buildAndExecRequest("GET", url)

	if code == 404 {
		return Issue{}, JiraError{fmt.Sprintf("Issue [%s] has not been found", id)}
	}

	var issue Issue
	err := json.Unmarshal(contents, &issue)
	if err != nil {
		fmt.Println("%s", err)
	}

	return issue, nil
}

func (j *Jira) createRequestWithAttachment(url string, path string) (resp *http.Response, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(part, file)

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, body)

	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.SetBasicAuth(j.Auth.Login, j.Auth.Password)
	req.Header.Add("X-Atlassian-Token", "nocheck")

	if err != nil {
		log.Fatal(err)
	}
	client := &http.Client{}

	resp, err = client.Do(req)
	if err != nil {
		log.Fatal(err)
	} else {
		body := &bytes.Buffer{}
		_, err := body.ReadFrom(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
	}

	return resp, err
}

func (j *Jira) UpdateAttachment(issueKey string, path string) error {

	attachmentId, err := j.FindAttachment(issueKey, path)

	if err != nil {
		return err
	}

	if attachmentId != "" {
		j.RemoveAttachment(attachmentId)
	}

	return j.AddAttachment(issueKey, path)
}

func (j *Jira) AddAttachment(issueKey string, path string) error {
	url := j.BaseUrl + j.ApiPath + "/issue/" + issueKey + "/attachments"
	hasAttachment, err := j.HasAttachment(issueKey, filepath.Base(path))

	if err != nil {
		return err
	}

	if hasAttachment {
		return JiraError{fmt.Sprintf("Jira issue %s already has XLA attachment", issueKey)}
	}

	resp, err := j.createRequestWithAttachment(url, path)

	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return JiraError{fmt.Sprintf("Failed to add attachment. Status code is %d", resp.StatusCode)}
	}

	return nil
}

func (j *Jira) DownloadAttachment(issueId string, attachmentFileName string) (string, error) {

	issue, err := j.Issue(issueId)
	if err != nil {
		return "", err
	}

	for _,attachment := range issue.Fields.Attachment {
		if strings.EqualFold(attachment.Filename, attachmentFileName) {
			j.downloadFromUrl(attachment.Content, attachmentFileName)
			pwd, err := os.Getwd()
			return pwd + string(os.PathSeparator) + attachmentFileName, err
		}
	}

	return "", JiraError{"Attachment hasn't been found"}
}

func (j *Jira) HasAttachment(issueKey string, attachmentFileName string) (bool, error) {
	attachmentId, err := j.FindAttachment(issueKey, attachmentFileName)

	if err != nil {
		return false, err
	}

	return attachmentId != "", nil
}

func (j *Jira) FindAttachment(issueKey string, attachmentFileName string) (string, error) {
	issue, err := j.Issue(issueKey)
	if err != nil {
		return "", err
	}

	for _,attachment := range issue.Fields.Attachment {
		if strings.EqualFold(attachment.Filename, filepath.Base(attachmentFileName)) {
			return attachment.Id, nil
		}
	}

	return "", JiraError{fmt.Sprintf("Attachment %s has not been found", attachmentFileName)}
}

func (j *Jira) downloadFromUrl(url string, fileName string) {
	fmt.Println("Downloading", url, "to", fileName)

	if j.isExist(fileName) {
		os.Remove(fileName)
	}

	output, err := os.Create(fileName)
	if err != nil {
		log.Fatal("Error while creating", fileName, "-", err)
		return
	}
	defer output.Close()

	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(j.Auth.Login, j.Auth.Password)
	response, err := j.Client.Do(req)

	if err != nil {
		log.Fatal("Error while downloading", url, "-", err)
		return
	}
	defer response.Body.Close()

	n, err := io.Copy(output, response.Body)
	if err != nil {
		log.Fatal("Error while downloading", url, "-", err)
		return
	}

	log.Print(n, " bytes downloaded.")
}

func (j *Jira) isExist(filename string) bool {
	_, err := os.Stat(filename);
	return !os.IsNotExist(err)
}

func (j *Jira) RemoveAttachment(issueId string) bool {

	url := j.BaseUrl + j.ApiPath + "/attachment/" + issueId
	_, code := j.buildAndExecRequest("DELETE", url)

	return code == 204
}
