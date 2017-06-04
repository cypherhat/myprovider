package main

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
)

func pkiResource() *schema.Resource {
	return &schema.Resource{
		Create: pkiWrite,
		Delete: pkiDelete,
		Read:   pkiRead,

		Schema: map[string]*schema.Schema{
			"common_name": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Common name of certificate",
			},
			"path": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "vault_intermediate",
				Description: "Path to the Certificate Authority",
			},
			"alt_names": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Subject Alt Names, comma delimited",
			},
			"ip_sans": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "IP Subject Alt Names, comma delimited",
			},
			"ttl": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "TTL for certificate",
			},
			"certificate": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The certificate",
			},
			"private_key": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The private key",
			},
			"issuing_ca": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The issuing CA",
			},
			"private_key_type": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The private key type",
			},
			"serial_number": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The serial number",
			},
			"revocation_time": &schema.Schema{
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "The revocation time",
			},
		},
	}
}

func pkiWrite(d *schema.ResourceData, meta interface{}) error {
	context := meta.(*ResourceContext)
	client := *context.client

	commonName := d.Get("common_name").(string)
	path := d.Get("path").(string)
	altNames := d.Get("alt_names").(string)
	ipSANS := d.Get("ip_sans").(string)
	ttl := d.Get("ttl").(string)

	log.Printf("[DEBUG] Issuing %s certificate", commonName)
	var data map[string]interface{}
	data = make(map[string]interface{})
	data["common_name"] = commonName
	if altNames != "" {
		data["alt_names"] = altNames
	}
	if ipSANS != "" {
		data["ip_sans"] = ipSANS
	}
	log.Printf("[DEBUG] TTL %s ", ttl)
	if ttl != "" {
		data["ttl"] = ttl
	}
	uri := path + "/issue/" + context.githubOrg
	secret, err := client.Logical().Write(uri, data)

	if err != nil {
		return fmt.Errorf("error writing to Vault: %s", err)
	}
	log.Print(secret.Data)
	id := secret.Data["serial_number"].(string)
	d.SetId(id)
	d.Set("certificate", secret.Data["certificate"])
	d.Set("private_key", secret.Data["private_key"])
	d.Set("issuing_ca", secret.Data["issuing_ca"])
	d.Set("private_key_type", secret.Data["private_key_type"])
	d.Set("serial_number", secret.Data["serial_number"])

	return nil
}

func pkiDelete(d *schema.ResourceData, meta interface{}) error {
	context := meta.(*ResourceContext)
	client := *context.client

	id := d.Id()
	path := d.Get("path").(string)
	var data map[string]interface{}
	data = make(map[string]interface{})
	data["serial_number"] = id
	log.Printf("[DEBUG] Revoking %s certificate", id)
	uri := path + "/revoke"
	secret, err := client.Logical().Write(uri, data)
	if err != nil {
		return fmt.Errorf("error writing to Vault: %s", err)
	}

	d.Set("revocation_time", secret.Data["revocation_time"])
	return nil
}

func pkiRead(d *schema.ResourceData, meta interface{}) error {

	return nil
}
