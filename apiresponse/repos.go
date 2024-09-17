package apiresponse

type Repos []Repo

type Repo struct {
    Url string `json:"url"`
}
