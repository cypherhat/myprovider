package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/hashicorp/vault/api"
)

type ResourceContext struct {
	client          *api.Client
	githubOrg       string
	namespaceDomain string
}

func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"address": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("VAULT_ADDR", nil),
				Description: "URL of the root of the target Vault server.",
			},
			"token": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("VAULT_TOKEN", ""),
				Description: "Token to use to authenticate to Vault.",
			},
			"personal_access_token": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "GitHub Token to use to authenticate to Vault.",
			},
			"github_org": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "GitHub Org to use to authenticate to Vault.",
			},
			"namespace_domain": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Namespace",
			},
			"ca_cert_file": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("VAULT_CACERT", ""),
				Description: "Path to a CA certificate file to validate the server's certificate.",
			},
			"ca_cert_dir": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("VAULT_CAPATH", ""),
				Description: "Path to directory containing CA certificate files to validate the server's certificate.",
			},
			"client_auth": &schema.Schema{
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Client authentication credentials.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"cert_file": &schema.Schema{
							Type:        schema.TypeString,
							Required:    true,
							DefaultFunc: schema.EnvDefaultFunc("VAULT_CLIENT_CERT", ""),
							Description: "Path to a file containing the client certificate.",
						},
						"key_file": &schema.Schema{
							Type:        schema.TypeString,
							Required:    true,
							DefaultFunc: schema.EnvDefaultFunc("VAULT_CLIENT_KEY", ""),
							Description: "Path to a file containing the private key that the certificate was issued for.",
						},
					},
				},
			},
			"skip_tls_verify": &schema.Schema{
				Type:        schema.TypeBool,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("VAULT_SKIP_VERIFY", ""),
				Description: "Set this to true only if the target Vault server is an insecure development instance.",
			},
			"max_lease_ttl_seconds": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,

				// Default is 20min, which is intended to be enough time for
				// a reasonable Terraform run can complete but not
				// significantly longer, so that any leases are revoked shortly
				// after Terraform has finished running.
				DefaultFunc: schema.EnvDefaultFunc("TERRAFORM_VAULT_MAX_TTL", 1200),

				Description: "Maximum TTL for secret leases requested by this provider",
			},
		},

		ConfigureFunc: providerConfigure,

		DataSourcesMap: map[string]*schema.Resource{
			"immutability_secret": genericSecretDataSource(),
		},

		ResourcesMap: map[string]*schema.Resource{
			"immutability_secret":  genericSecretResource(),
			"immutability_policy":  policyResource(),
			"immutability_ssl":     pkiResource(),
			"immutability_approle": approleResource(),
		},
	}
}

func githubLogin(d *schema.ResourceData) (string, error) {
	address := d.Get("address").(string)
	githubOrg := d.Get("github_org").(string)
	namespaceDomain := d.Get("namespace_domain").(string)

	personalAccessToken := d.Get("personal_access_token").(string)
	if personalAccessToken == "" || githubOrg == "" || namespaceDomain == "" {
		return "", errors.New("Missing personal_access_token or github_org or namespace_domain")
	}

	githubPath := "github/" + namespaceDomain + "/" + githubOrg
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	vaultCaCertFile := d.Get("ca_cert_file").(string)
	log.Printf("[DEBUG] CACert File %s", vaultCaCertFile)
	if vaultCaCertFile != "" {
		caCert, err := ioutil.ReadFile(vaultCaCertFile)
		if err != nil {
			return "", err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		}
	}
	vaultGitHubURL := address + "/v1/auth/" + githubPath + "/login"
	log.Printf("[DEBUG] GitHub Login URL %s", vaultGitHubURL)
	var jsonStr = []byte(`{"token":"` + personalAccessToken + `"}`)
	authRequest, _ := http.NewRequest("POST", vaultGitHubURL, bytes.NewBuffer(jsonStr))
	resp, err := client.Do(authRequest)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("No response from vault during approle auth")
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Vault authentication to GitHub Status %d", resp.StatusCode)
	}
	var payload api.Secret
	var htmlData []byte
	if resp != nil {
		htmlData, _ = ioutil.ReadAll(resp.Body)
	}
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(htmlData, &payload)
	if err != nil {
		return "", err
	}
	return payload.Auth.ClientToken, nil

}

func providerConfigure(d *schema.ResourceData) (interface{}, error) {

	config := api.DefaultConfig()
	config.Address = d.Get("address").(string)

	clientAuthI := d.Get("client_auth").([]interface{})
	if len(clientAuthI) > 1 {
		return nil, fmt.Errorf("client_auth block may appear only once")
	}

	clientAuthCert := ""
	clientAuthKey := ""
	if len(clientAuthI) == 1 {
		clientAuth := clientAuthI[0].(map[string]interface{})
		clientAuthCert = clientAuth["cert_file"].(string)
		clientAuthKey = clientAuth["key_file"].(string)
	}

	err := config.ConfigureTLS(&api.TLSConfig{
		CACert:   d.Get("ca_cert_file").(string),
		CAPath:   d.Get("ca_cert_dir").(string),
		Insecure: d.Get("skip_tls_verify").(bool),

		ClientCert: clientAuthCert,
		ClientKey:  clientAuthKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to configure TLS for Vault API: %s", err)
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to configure Vault API: %s", err)
	}

	token := d.Get("token").(string)
	personalAccessToken := d.Get("personal_access_token").(string)

	if personalAccessToken != "" {
		log.Println("[DEBUG] Using GitHub Login")
		token, err = githubLogin(d)
		if err != nil {
			log.Println("[ERROR] GitHub Login Failed")
			return nil, err
		}

	}
	if token == "" {
		return nil, fmt.Errorf("No authentication token was supplied!")
	}
	client.SetToken(token)
	var context ResourceContext
	context.client = client
	context.githubOrg = d.Get("github_org").(string)
	context.namespaceDomain = d.Get("namespace_domain").(string)
	return &context, nil
}
