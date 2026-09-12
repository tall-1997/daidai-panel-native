package middleware

import (
	"github.com/gin-gonic/gin"
)

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
		// CSP script-src retains 'unsafe-eval' because Monaco Editor's loader.js
		// uses self.eval() and new Function() for dynamic module instantiation
		// (see node_modules/monaco-editor/min/vs/loader.js). Removing it breaks
		// the code/diff editor in the panel web UI. Risk: eval() is an XSS vector,
		// but all script sources are constrained to 'self' and cdn.jsdelivr.net.
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; img-src 'self' data: blob:; font-src 'self' data: https://cdn.jsdelivr.net; connect-src 'self' ws: wss: https://cdn.jsdelivr.net; worker-src 'self' blob:")
		c.Next()
	}
}
