package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
)

func approleResource() *schema.Resource {
	return &schema.Resource{
		Create: approleWrite,
		Delete: approleDelete,
		Read:   approleRead,

		Schema: map[string]*schema.Schema{
			"repository": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of GitHub repository",
			},
			"secret_id": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The AppRole Secret ID",
			},
			"role_id": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "AppRole Role ID",
			},
			"auth_path": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "AppRole Login path",
			},
		},
	}
}

func approleWrite(d *schema.ResourceData, meta interface{}) error {
	context := meta.(*ResourceContext)
	client := *context.client
	var data map[string]interface{}
	data = make(map[string]interface{})

	repository := d.Get("repository").(string)

	path := "auth/approle/" + context.namespaceDomain + "/" + context.githubOrg + "/role/" + repository
	log.Printf("[DEBUG] Reading RoleID for %s", path)

	secretRole, err := client.Logical().Read(path + "/role-id")

	if err != nil || secretRole == nil {
		return fmt.Errorf("Error reading RoleID from Vault: %s", err)
	}
	if _, present := secretRole.Data["role_id"]; !present {
		return errors.New("RoleID not found")
	}

	roleID := secretRole.Data["role_id"].(string)
	log.Printf("[DEBUG] Got RoleID = %s", roleID)
	secret, err := client.Logical().Write(path+"/secret-id", data)

	if err != nil || secret == nil {
		return fmt.Errorf("Error generating SecretID from Vault: %s", err)
	}

	if _, present := secret.Data["secret_id"]; !present {
		return errors.New("secretID not found")
	}

	secretID := secret.Data["secret_id"].(string)
	log.Printf("[DEBUG] Got secretID = %s", secretID)

	d.SetId(path)
	d.Set("role_id", roleID)
	d.Set("secret_id", secretID)
	d.Set("auth_path", client.Address()+"/v1/auth/approle/"+context.namespaceDomain+"/"+context.githubOrg+"/login")
	return nil
}

func approleDelete(d *schema.ResourceData, meta interface{}) error {
	context := meta.(*ResourceContext)
	client := *context.client

	path := d.Id()
	secretID := d.Get("secret_id")
	var data map[string]interface{}
	data = make(map[string]interface{})
	data["secret_id"] = secretID
	log.Printf("[DEBUG] Revoking secret_id at %s ", path)
	uri := path + "/secret-id/destroy"
	_, err := client.Logical().Write(uri, data)
	if err != nil {
		return fmt.Errorf("Error Revoking secret_id: %s", err)
	}

	return nil
}

func approleRead(d *schema.ResourceData, meta interface{}) error {

	return nil
}
