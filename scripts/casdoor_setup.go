package main

import (
	"flag"
	"fmt"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
)

func main() {
	globals.Parse()
	config.Init()
	cd := config.GetConfig().Casdoor
	casdoorsdk.InitConfig(cd.Endpoint, cd.ClientID, cd.ClientSecret, cd.Certificate, cd.OrganizationName, cd.ApplicationName)

	action := flag.Arg(0)
	switch action {
	case "create-roles":
		createRoles()
	case "assign-users":
		assignUsers()
	case "list-roles":
		listRoles()
	case "list-users":
		listUsers()
	default:
		fmt.Println("Unknown action:", action)
	}
}

func createRoles() {
	roles := []struct {
		Name        string
		DisplayName string
		Description string
	}{
		{"pfm_admin", "PFM管理员", "拥有PFM应用全部权限"},
		{"pfm_user", "PFM普通用户", "受目录权限约束的普通用户"},
	}
	for _, r := range roles {
		role := &casdoorsdk.Role{
			Owner:       "admin",
			Name:        r.Name,
			DisplayName: r.DisplayName,
			Description: r.Description,
			IsEnabled:   true,
		}
		_, err := casdoorsdk.AddRole(role)
		if err != nil {
			fmt.Printf("Create role %s failed (may already exist): %v\n", r.Name, err)
		} else {
			fmt.Printf("Created role: %s\n", r.Name)
		}
	}
}

func assignUsers() {
	assignments := []struct {
		RoleName string
		UserName string
	}{
		{"pfm_admin", "pfm_admin@0259.eu.org"},
		{"pfm_user", "pfm_user1@<域名>"},
	}
	for _, a := range assignments {
		role, err := casdoorsdk.GetRole("admin/" + a.RoleName)
		if err != nil {
			fmt.Printf("Get role %s failed: %v\n", a.RoleName, err)
			continue
		}
		// 检查用户是否已在角色中
		found := false
		for _, u := range role.Users {
			if u == a.UserName {
				found = true
				break
			}
		}
		if found {
			fmt.Printf("User %s already in role %s\n", a.UserName, a.RoleName)
			continue
		}
		role.Users = append(role.Users, a.UserName)
		_, err = casdoorsdk.UpdateRole(role)
		if err != nil {
			fmt.Printf("Assign %s to %s failed: %v\n", a.UserName, a.RoleName, err)
		} else {
			fmt.Printf("Assigned %s -> %s\n", a.UserName, a.RoleName)
		}
	}
}

func listRoles() {
	roles, err := casdoorsdk.GetRoles()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for _, r := range roles {
		fmt.Printf("Role: %s (%s) users=%v\n", r.Name, r.DisplayName, r.Users)
	}
}

func listUsers() {
	users, err := casdoorsdk.GetUsers()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for _, u := range users {
		fmt.Printf("User: %s (%s) type=%s\n", u.Name, u.Email, u.Type)
	}
}

