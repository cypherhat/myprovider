# The Immutability Terraform plugin

This is a [native Terraform plugin](https://www.terraform.io/docs/plugins/basics.html). The purpose of the plugin is to provide an application with secrets during provisioning. This includes:

* SSL Certificates
* Generic secrets
* Tokens

This provider was based on the built-in Terraform Vault provider. As such, it provides the same capabilities; however, the `immutability` provider is opinionated. The biggest opinion that this provider has is related to authentication. This provider assumes that you are using a GitHub personal access token for authentication. The built-in Terraform Vault provider only supports authentication via Vault tokens.

The `immutability` provider also provides a capability missing from the built-in Terraform Vault provider: the ability to generate an SSL keypair. Our provider is opinionated as regards defaults related to the pathing of the CA and the role used to issue certificates. The intent is to provide a simple API for developers to create SSL certs.

### Configure the plugin

You need to tell terraform where to find the plugin. You do this by creating a file called `.terraformrc` and putting it in your home directory:

```
$ cat ~/.terraformrc

$ providers {
    immutability = "/projects/plugins/terraform-provider-immutability"
}
```

## Provider Arguments
The provider configuration block accepts the following arguments. In most cases it is recommended to set them via the indicated environment variables in order to keep credential information out of the configuration.

### Example usage

```
provider "immutability" {
  personal_access_token = "${var.personal_access_token}"
  github_org = "${var.github_org}"
  address = "${var.vault_addr}"
}
```

`address` - (Required) Origin URL of the Vault server. This is a URL with a scheme, a hostname and a port but with no path. May be set via the VAULT_ADDR environment variable.

`token` - (Optional) If present, used by Terraform to authenticate. May be set via the VAULT_TOKEN environment variable. If none is otherwise supplied, Terraform will attempt to read it from ~/.vault-token (where the vault command stores its current token).

`personal_access_token` - (Optional) This is the preferred authentication token according to the opinions of this provider. If the Vault token is *not* present, the `personal_access_token` is used by Terraform to authenticate. This will be used in concert with the `github_org` to establish a level of trust with Vault. Please read [On GitHub and Vault]()

`ca_cert_file` - (Optional) Path to a file on local disk that will be used to validate the certificate presented by the Vault server. May be set via the VAULT_CACERT environment variable.
## Resources

### Resource: immutability_ssl

`immutability_ssl` - The `immutability_ssl` resource generates X.509 keypairs dynamically. This means services can get certificates needed for both client and server authentication without going through the usual manual process of generating a private key and CSR, submitting to a CA, and waiting for a verification and signing process to complete. Vault's built-in authentication and authorization mechanisms provide the verification functionality.

By keeping TTLs relatively short, revocations are less likely to be needed, keeping CRLs short and helping the backend scale to large workloads. This in turn allows each instance of a running application to have a unique certificate, eliminating sharing and the accompanying pain of revocation and rollover.

In addition, by allowing revocation to mostly be forgone, this backend allows for ephemeral certificates; certificates can be fetched and stored in memory upon application startup and discarded upon shutdown, without ever being written to disk. That said, this resource will **revoke the certificate** when the resource is destroyed.

### Example usage

```

resource "immutability_ssl" "my-repository_certificates" {
    ttl="8760h"
    ip_sans = "127.0.0.1"
    alt_names = "*.immutability.io,localhost"
    common_name="my-repository.${var.domain_name}"
}


resource "aws_iam_server_certificate" "my-repository_service_certificate" {
    name_prefix      = "my-repository"
    certificate_body = "${immutability_ssl.my-repository_certificates.certificate}"
    private_key      = "${immutability_ssl.my-repository_certificates.private_key}"

    lifecycle {
        create_before_destroy = true
    }
}


```

### immutability_ssl: Argument Reference

`common_name` - (Required) The requested CN for the certificate.

`alt_names` - (Optional)  Requested Subject Alternative Names, in a comma-delimited list. These can be host names or email addresses; they will be parsed into their respective fields.

`ip_sans` - (Optional) Requested IP Subject Alternative Names, in a comma-delimited list. Only valid if the role allows IP SANs (which is the default).

`ttl` - (Optional) Requested Time To Live. Cannot be greater than the role's max_ttl value. If not provided, the default ttl value will be used.

### immutability_ssl: Attributes Reference

The following attributes are exported:

`certificate` - The PEM encoded X.509 certificate.

`private_key` - The PEM encoded private key.

`private_key_type` - Defaults to rsa. A future release will allow `ec` to be requested.

`serial_number` - The certificates serial number. This is used for revocation.

## immutability_approle

### AppRoles

An AppRole represents a set of Vault policies and login constraints that must be met to receive a token with those policies. The scope can be as narrow or broad as desired -- an AppRole can be created for a particular machine, or even a particular user on that machine, or a service spread across machines. The credentials required for successful login depend upon on the constraints set on the AppRole associated with the credentials.

### Credentials/Constraints

#### RoleID

RoleID is an identifier that selects the AppRole against which the other credentials are evaluated. When authenticating against this backend's login endpoint, the RoleID is a required argument (via role_id) at all times. By default, RoleIDs are unique UUIDs, which allow them to serve as secondary secrets to the other credential information. However, they can be set to particular values to match introspected information by the client (for instance, the client's domain name).

#### SecretID

SecretID is a credential that is required by default for any login (via secret_id) and is intended to always be secret. (For advanced usage, requiring a SecretID can be disabled via an AppRole's bind_secret_id parameter, allowing machines with only knowledge of the RoleID, or matching other set constraints, to fetch a token). Similarly to tokens, SecretIDs have properties like usage-limit, TTLs and expirations. Secrets are bound to specific CIDR blocks as well.

### Opinionated Implementation

This implementation attempts to make provisioning secrets for applications simple for development teams. The idea is that access to secrets (e.g., database credentials, API keys, etc.) is mapped to specific deployments of an application. For example, when an application is deployed to a production environment, only that application in that environment is allowed to access production secrets. Neither the developer nor the deployer has access to these secrets.

To do this, we only require one parameter:

`repository` - This is where the source code for the application lives.

**Assuming that the application's secrets have been provisioned**, all the developer/deployer needs to know to request AppRole tokens for an application is the name of the repository where the application source code lives.

### immutability_approle: Argument Reference

`repository` - This is where the source code for the application lives.

### immutability_approle: Attributes Reference

The following attributes are exported:

`role_id` - See RoleID discussion above. This is used with `secret_id` to authenticate.

`secret_id` - See SecretID discussion above. This is used with `role_id` to authenticate.

`auth_path` - The path used by the application to authenticate. The application will make a REST call to this endpoint with the `role_id` and `secret_id` to receive a Vault token.

### Example usage

```
resource "immutability_approle" "my_app_secret"{
  repository = "${var.repository}"
}


module "reference_app" {
...
  secret_id = "${immutability_approle.my_app_secret.secret_id}"
  role_id = "${immutability_approle.my_app_secret.role_id}"
  auth_path = "${immutability_approle.my_app_secret.auth_path}"
...  
}

```

The `reference_app` referred to in the above snippet refers to the **application** being deployed. This **application** will authenticate to Vault at the endpoint returned in `auth_path`:

```
# role_id == 8a31cd9d-1106-ebf3-9d55-544c5263fc67
# secret_id == 598b2134-6f35-ffd7-3396-3803f148cea5
# auth_path == https://vault.dev.immutability.io:8200/v1/auth/approle/immutability.com/my-github-org/login

$ curl -X POST \
     -d '{"role_id":"8a31cd9d-1106-ebf3-9d55-544c5263fc67","secret_id":"598b2134-6f35-ffd7-3396-3803f148cea5"}' \
     https://vault.dev.immutability.io:8200/v1/auth/approle/immutability.com/my-github-org/login | jq .
```

### immutability_approle: Argument Reference

`repository` - (Required) This is where the source code for the application lives.

### immutability_approle: Attributes Reference

`secret_id` - The `secret_id` that the application or service will use (in combination with the `role_id`) to authenticate to vault.

`role_id` - The `role_id` that the application or service will use (in combination with the `secret_id`) to authenticate to vault.

### Resource: immutability_secret

`immutability_secret` - Please see [HashiCorp's Vault Provider Documentation](https://www.terraform.io/docs/providers/vault/r/generic_secret.html). The `immutability_secret` resource inherits the behavior of `vault_generic_secret` resource.

### Resource: immutability_policy

`immutability_policy` - Please see [HashiCorp's Vault Provider Documentation](https://www.terraform.io/docs/providers/vault/r/policy.html). The `immutability_policy` resource inherits the behavior of `vault_policy` resource.

## Data Sources

### DataSource: immutability_secret

`immutability_secret` - Please see [HashiCorp's Vault Provider Documentation](https://www.terraform.io/docs/providers/vault/d/generic_secret.html). The `immutability_secret` data source inherits the behavior of `vault_generic_secret` data source .

## Testing

I use the following Terraform template for testing (on a Mac or in a Ubuntu vagrant box.) **Note:** some of your variables will be different based on your environment.

```

variable "personal_access_token" {default="supersecret" }
variable "github_org" { default = "my-github-org" }
variable "repository" { default = "my-repository" }
variable "vault_addr" { default = "https://localhost:8200" }
variable "ca_cert_file" { default = "/etc/vault.d/root.crt" }

provider "immutability" {
  personal_access_token = "${var.personal_access_token}"
  github_org = "${var.github_org}"
  address = "${var.vault_addr}"
}

data "immutability_secret" "gossip_encryption_key" {
  path = "secret/my-domain/${var.github_org}/${var.repository}/gossip_encryption_key"
}

resource "immutability_ssl" "my_certs" {
  ip_sans = "127.0.0.1"
  ttl = "8760h"
  alt_names = "*.immutability.io,localhost"
  common_name="my-repository.immutability.io"
}

resource "immutability_approle" "my_app_secret"{
  repository = "${var.repository}"
}
output "certificate" {
  value ="${immutability_ssl.my_certs.certificate}"
}

output "private_key" {
  value ="${immutability_ssl.my_certs.private_key}"
}

output "issuing_ca" {
  value ="${immutability_ssl.my_certs.issuing_ca}"
}

output "private_key_type" {
  value ="${immutability_ssl.my_certs.private_key_type}"
}

output "serial_number" {
  value ="${immutability_ssl.my_certs.serial_number}"
}

output "role_id" {
  value ="${immutability_approle.my_app_secret.role_id}"
}

output "secret_id" {
  value ="${immutability_approle.my_app_secret.secret_id}"
}

output "auth_path" {
  value ="${immutability_approle.my_app_secret.auth_path}"
}

```
