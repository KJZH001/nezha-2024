package model

type UserForm struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty" gorm:"type:char(72)"`
}
