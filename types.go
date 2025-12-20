package main

import (
	"encoding/xml"

	"github.com/go-rod/rod"
)

type state struct {
	name     string
	selector string
	handler  func(pg *rod.Page, el *rod.Element, ctx *HandlerContext)
}

type samlResponse struct {
	XMLName   xml.Name
	Assertion samlAssertion `xml:"Assertion"`
}

type samlAssertion struct {
	XMLName            xml.Name
	AttributeStatement samlAttributeStatement
}

type samlAttributeValue struct {
	XMLName xml.Name
	Type    string `xml:"xsi:type,attr"`
	Value   string `xml:",innerxml"`
}

type samlAttribute struct {
	XMLName         xml.Name
	Name            string               `xml:",attr"`
	AttributeValues []samlAttributeValue `xml:"AttributeValue"`
}

type samlAttributeStatement struct {
	XMLName    xml.Name
	Attributes []samlAttribute `xml:"Attribute"`
}

type role struct {
	roleArn      string
	principalArn string
}

type LoginOptions struct {
	NoPrompt         bool
	IsGui            bool
	ShowBrowser      bool
	DisableLeakless  bool
	FastPass         bool
	UseSystemBrowser bool
	AwsNoVerifySsl   bool
	ForceRefresh     bool
}

type HandlerContext struct {
	DefaultUserName     string
	DefaultUserPassword *string
	DefaultOktaUserName *string
	DefaultOktaPassword *string
	NoPrompt            bool
	IsGui               bool
}
