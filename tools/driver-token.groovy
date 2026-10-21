// Local admin-only bootstrap invoked through the authenticated script API.
// The response is captured privately by driver-token.ps1, never printed.
import hudson.model.User
import jenkins.security.ApiTokenProperty
def user = User.get(System.getenv('JENKINS_ADMIN_USER'), false)
if (user == null) { throw new IllegalStateException('Configured Jenkins admin is missing') }
def property = user.getProperty(ApiTokenProperty)
def existing = property.tokenStore.tokenList.findAll { it.name == 'buildlens-local-driver' }
existing.each { property.tokenStore.revokeToken(it.uuid) }
def generated = property.tokenStore.generateNewToken('buildlens-local-driver')
user.save()
print generated.plainValue
