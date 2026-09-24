package stealth

import (
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	rodstealth "github.com/go-rod/stealth"
)

var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
}

var viewports = [][2]int{
	{1920, 1080},
	{1366, 768},
	{1536, 864},
	{1440, 900},
}

type ProxyAuth struct {
	Username string
	Password string
}

type Config struct {
	ProxyServer string
	ProxyAuth   *ProxyAuth
	UserAgent   string
	Viewport    [2]int
	ChromePath  string
}

func NewConfig() Config {
	return Config{
		UserAgent:  randomUA(),
		Viewport:   randomViewport(),
		ChromePath: os.Getenv("CHROME_PATH"),
	}
}

func (c Config) WithProxy(proxyURL *string) Config {
	if proxyURL == nil || *proxyURL == "" {
		return c
	}
	parsed, err := url.Parse(*proxyURL)
	if err != nil {
		c.ProxyServer = *proxyURL
		return c
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		password, _ := parsed.User.Password()
		if username != "" {
			c.ProxyAuth = &ProxyAuth{Username: username, Password: password}
			parsed.User = nil
			c.ProxyServer = parsed.String()
			return c
		}
	}
	c.ProxyServer = *proxyURL
	return c
}

type Session struct {
	Browser *rod.Browser
	cleanup func()
}

func Launch(cfg Config) (*Session, error) {
	l := launcher.New().
		Headless(true).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-infobars", "").
		Set("no-first-run", "").
		Set("no-default-browser-check", "").
		Set("disable-gpu", "").
		Set("disable-dev-shm-usage", "").
		Set("lang", "en-US,en").
		Set("window-size", fmt.Sprintf("%d,%d", cfg.Viewport[0], cfg.Viewport[1]))

	if cfg.ChromePath != "" {
		l = l.Bin(cfg.ChromePath)
	}
	if cfg.ProxyServer != "" {
		l = l.Proxy(cfg.ProxyServer)
	}

	controlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch stealth browser: %w", err)
	}

	browser := rod.New().ControlURL(controlURL).Timeout(120 * time.Second)
	if err := browser.Connect(); err != nil {
		l.Kill()
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	if cfg.ProxyAuth != nil {
		go func() {
			_ = browser.HandleAuth(cfg.ProxyAuth.Username, cfg.ProxyAuth.Password)()
		}()
	}

	return &Session{
		Browser: browser,
		cleanup: func() {
			_ = browser.Close()
			l.Kill()
		},
	}, nil
}

func (s *Session) Close() {
	if s != nil && s.cleanup != nil {
		s.cleanup()
	}
}

func NewPage(session *Session, cfg Config) (*rod.Page, error) {
	page, err := stealthPage(session.Browser)
	if err != nil {
		return nil, err
	}
	if err := Apply(page, cfg); err != nil {
		return nil, err
	}
	return page, nil
}

func stealthPage(browser *rod.Browser) (*rod.Page, error) {
	page, err := rodstealth.Page(browser)
	if err != nil {
		return nil, fmt.Errorf("new stealth page: %w", err)
	}
	return page, nil
}

func Apply(page *rod.Page, cfg Config) error {
	w, h := cfg.Viewport[0], cfg.Viewport[1]
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             w,
		Height:            h,
		DeviceScaleFactor: 1,
		Mobile:            false,
	}); err != nil {
		return fmt.Errorf("set viewport: %w", err)
	}

	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: cfg.UserAgent,
	}); err != nil {
		return fmt.Errorf("set user agent: %w", err)
	}

	if _, err := page.Eval(stealthJS(w, h)); err != nil {
		return fmt.Errorf("failed to inject stealth JS: %w", err)
	}
	return nil
}

func Navigate(page *rod.Page, rawURL string, waitSecs uint64, cfg Config) (string, error) {
	if err := page.Navigate(rawURL); err != nil {
		return "", fmt.Errorf("navigation failed: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return "", fmt.Errorf("wait navigation failed: %w", err)
	}

	if waitSecs > 0 {
		time.Sleep(time.Duration(waitSecs) * time.Second)
	}

	if _, err := page.Element("body"); err != nil {
		return "", fmt.Errorf("no body element: %w", err)
	}

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get content: %w", err)
	}
	return html, nil
}

func randomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func randomViewport() [2]int {
	return viewports[rand.Intn(len(viewports))]
}

func stealthJS(w, h int) string {
	return fmt.Sprintf(`() => {
const patch = (obj, prop, getter) => {
    try {
        Object.defineProperty(obj, prop, { get: getter, configurable: true });
    } catch (e) {}
};
patch(navigator, 'webdriver', () => undefined);
patch(navigator, 'plugins', () => {
    const plugins = [
        { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
        { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
        { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' },
    ];
    plugins.length = 3;
    return plugins;
});
patch(navigator, 'languages', () => ['en-US', 'en']);
patch(navigator, 'hardwareConcurrency', () => 8);
patch(navigator, 'deviceMemory', () => 8);
patch(screen, 'width', () => %d);
patch(screen, 'height', () => %d);
patch(screen, 'availWidth', () => %d);
patch(screen, 'availHeight', () => %d);
window.chrome = window.chrome || { runtime: { connect: () => {}, sendMessage: () => {} } };
try {
    for (const key of Object.keys(window)) {
        if (key.startsWith('cdc_') || key.startsWith('__webdriver')) {
            delete window[key];
        }
    }
} catch (e) {}
}`, w, h, w, h-40)
}
