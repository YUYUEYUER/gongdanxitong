/**
 * Libredesk Chat Widget
 * Embeddable chat widget for websites
 */
(function () {
    'use strict';

    if (window.__libredeskWidgetLoaded) {
        return;
    }
    window.__libredeskWidgetLoaded = true;
    const DEFAULT_LAUNCHER_LOGO = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAMAAAD04JH5AAAAAXNSR0IB2cksfwAAAAlwSFlzAAALEwAACxMBAJqcGAAAAmRQTFRFAAAAERclERgnERgnERgnEBcoERgnEhgmERknEhcnERgnERgnERgnERgnERgnERgnERgnERgnERgnERgnERgnEhkoAD0wERgmERgnERgnERgnERgnERgnERgnERgnERgnERgnEhclERgmERgnERgnERgnERgnERgoERknERkmERgnERgnERgnERgnEhgnExkmERknERgnERgnERgnERcnERgnERgnERgnERgnERgnEBgnEhUcERgmERgnERgnERgnDA4uDBAcERgnERcoBw4vERgnERgnERgnERgnERgnERgoEBgoERcoERolERgnERooEhgnERgnEBkoERgnERknERgnERgnERgnERgnEhgoERgnEhcnERgnEhcmERgnEhgmERgnEhgmERgnERgnERgpERgoERgnERknERgnERgnERgnEhgnERgnERgnERgnERgnERcmEBcmERgnERgnEhknERgmERgnExYnEhkoEhgnERgnERgnEhgnERgnERgnEhgnERgnERgnERgnEhgnERgmEhkoERgnERgnERgnERgnERgnERcnERcnEhcnERgnEBgjERgnDxokERkmERgnERgnERgnERgoEBcoERgnERgnERYpERgmEhYdERgnERgnERgmDRIZDxUoERgnERgnERgnERknERgmERgnEBcnERgnERgnERgnEhcnEhcnERgnERgmERkmERgnERcoERgnEhknEhknERgnERgmERgnERgnERgnERgnEBknERgmERgmERgnEBcmFBcmERgnERcmERknERslERgnERgnDxkowSUfnQAAAMx0Uk5TAAcNExYZGA4HHUNrlbXP3+fmtpZsRR4BD0aRxuz/7ceTRxARW+/wu2EZBkux8/e6Twggiu6SIzi8/P3APgFS19pXAgHg5F4CUuFWOtY+HiIGiwhHThS3FvZfDrMQPhnCHURkagWMlAcMrw0Rxc4TFdje5RgZFN0TDK0EBmP+aTzrQhgchY49RA2utAie9B8TW0sFoAYp0135NwmhUSRVAWLUUAEHpvu5Ny6IHWPysAYKqBIo41uJPQ34FzusxMNiPRibCAQLFgwEci4RpmpYSgAAAzdJREFUeJzt12tIU2EYB/DzL41ULKxIP6hEfkiECALRELsgiVHZJAQzF80JGWaWIFpWgtjFUjMrpVBa4AVJyzIqK/ogRpnRBYphNyqUVKIsjdK52bbKlpk+r77Hvjz/D2PsPOf8f5ydvecMyn8OGMAABjCAAQxgAAMYwAAGMIABDGAAAxjAgInsa49l0Anf/gPABfgNsKVzMgFe9krHw9jyarIAfvgw+++TbjslxskABFiLukfc4mHd8lB1wGJ0/nsfT+CeyoAgoG2UzT5Ak5qA0LGvND/glmqAMMvU1jGH/IEGtQAReEKY8ne+ohJgNR6T5vzc61UBRHa1Eyd951xUAaBBx2jXv2N8Xgeflw9Y30yf9ZxXIx0QbfxE7leUJaiWDVjxfuT1d+SEoEoyIBaNAv2KMmthuVyA1ih2v3fpeScXEHVfqN/6yNAiFbBJbIG3xrLSIBOgrXcXBCDsjExA/A3BfkUJL5MISMA1YcAqnJIHSMRlYUC3tlgeIAn0u8uvaFAkD5CCWmHA5y8D8gBOyeSby1CikS8RsHzsR7Hh6dXnyQOkGWnPQo7ZUCDxK0iqmyIM0LjmygNkgHhvc8jSKos8wO6GLmGAV/h+eQBlDwyiAF02aYwIyOo5J9ivR5ZMQHbDW0HAOs+9MgFKTp9BDJCYSZujAg4Ui/2NnR6/Sy7gEE4IAZIfVcoFHMExkf4dJS+Ik1RAwaWXIoCZ+p2SAYV3bwv0D6alUEeJgCIcFuj37tdtkww4iYMCAFe3B+RZIqAEpIX9Rxat3UIfpgFOg7aw27MgJoE+TASUpc6g98dCJx1gKPhIPWAOoBXopwHKkUE8XC4QI1JPBFTfIT6V6wMQLdZPA9T0p1PGClERFyXYTwLU4Y9lJaRjjfm4zjBsaPPZwtLESNF6GqAeW4feZ1rM81vN5q/OV4PcHAy9AZlAhHg9DbAv5OcP2+NN3oDFvN3+/jra505D21PT81RLeh6wbDztREDjRturd1CgqTg/1OHzJlS6Hm28UFuB4HG2EwHN3QmlJrPJ91nc+HsmBGix9N0MNGtUaCcCVA0DGMAABjCAAQxgAAMYwAAGMIABDGAAA74DhTG6gQJjoUQAAAAASUVORK5CYII=';

    class Libredesk {
        constructor(config = {}) {
            if (!config.baseURL) {
                throw new Error('baseURL is required');
            }
            if (!config.inboxID) {
                throw new Error('inboxID is required');
            }
            if (!/^[A-Za-z0-9_-]{1,128}$/.test(String(config.inboxID))) {
                throw new Error('inboxID is invalid');
            }

            const parsedBaseURL = new URL(config.baseURL, window.location.href);
            if (parsedBaseURL.protocol !== 'http:' && parsedBaseURL.protocol !== 'https:') {
                throw new Error('baseURL must use HTTP or HTTPS');
            }
            if (parsedBaseURL.username || parsedBaseURL.password || parsedBaseURL.search || parsedBaseURL.hash) {
                throw new Error('baseURL must not contain credentials, query parameters, or fragments');
            }

            this.IFRAME_BORDER_RADIUS = '16px';
            this.IFRAME_BOX_SHADOW = '0 12px 48px rgba(0,0,0,0.35), 0 4px 16px rgba(0,0,0,0.25)';
            this.IFRAME_WIDTH = '400px';
            this.IFRAME_HEIGHT = '700px';
            this.EXPANDED_WIDTH = '750px';
            this.MOBILE_BREAKPOINT = 600;
            this.LAUNCHER_SIZE = 60;
            this.MOBILE_LAUNCHER_SIZE = 50;

            this.config = config;
            this.baseURL = parsedBaseURL.href.replace(/\/$/, '');
            this.widgetOrigin = parsedBaseURL.origin;
            this.channelNonce = this.generateChannelNonce();
            this.channelReady = false;
            this._channelInitTimer = null;
            this._channelInitAttempts = 0;
            this._logoutRequest = null;
            this._pendingMessages = [];
            this.iframe = null;
            this.toggleButton = null;
            this.widgetButtonWrapper = null;
            this.unreadBadge = null;
            this.isChatVisible = false;
            this.widgetSettings = null;
            this.unreadCount = 0;
            this.isMobile = window.innerWidth <= this.MOBILE_BREAKPOINT;
            this.isExpanded = false;
            this.hideLauncher = config.hideLauncher || false;
            this.widgetLoaded = false;
            this._onShowCallback = null;
            this._onHideCallback = null;
            this._onUnreadCountChangeCallback = null;
            this._boundHandleMessage = (e) => this.handleMessage(e);
            this._boundHandleResize = () => this.handleResize();
            this.init();
        }

        generateChannelNonce () {
            if (!window.crypto || typeof window.crypto.getRandomValues !== 'function') {
                throw new Error('Secure browser randomness is required');
            }
            const bytes = new Uint8Array(24);
            window.crypto.getRandomValues(bytes);
            return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
        }

        channelEnvelope (data) {
            return Object.assign({}, data, {
                bridge: 'libredesk-widget-bridge',
                version: 1,
                channelNonce: this.channelNonce
            });
        }

        postChannelInit () {
            if (!this.iframe || !this.iframe.contentWindow) return;
            this.iframe.contentWindow.postMessage(this.channelEnvelope({
                type: 'WIDGET_CHANNEL_INIT'
            }), this.widgetOrigin);
        }

        startChannelHandshake () {
            this.stopChannelHandshake();
            this._channelInitAttempts = 0;

            const attempt = () => {
                if (this.channelReady || !this.iframe || !this.iframe.contentWindow) {
                    this.stopChannelHandshake();
                    return;
                }
                this.postChannelInit();
                this._channelInitAttempts += 1;
                if (this._channelInitAttempts >= 80) {
                    this.stopChannelHandshake();
                    return;
                }
                this._channelInitTimer = setTimeout(attempt, 250);
            };

            attempt();
        }

        stopChannelHandshake () {
            if (this._channelInitTimer !== null) {
                clearTimeout(this._channelInitTimer);
                this._channelInitTimer = null;
            }
        }

        postToIframe (data) {
            if (!this.iframe || !this.iframe.contentWindow) return;
            if (!this.channelReady) {
                if (this._pendingMessages.length >= 100) this._pendingMessages.shift();
                this._pendingMessages.push(data);
                return;
            }
            this.iframe.contentWindow.postMessage(this.channelEnvelope(data), this.widgetOrigin);
        }

        flushPendingMessages () {
            while (this.channelReady && this._pendingMessages.length > 0) {
                const data = this._pendingMessages.shift();
                this.iframe.contentWindow.postMessage(this.channelEnvelope(data), this.widgetOrigin);
            }
        }

        formatBadgeCount (count) {
            return count > 99 ? '99+' : count.toString();
        }

        getCookieName (type) {
            return 'libredesk-' + type + '-' + this.config.inboxID;
        }

        getLegacyCookieDomains () {
            const domains = new Set();
            const hostname = window.location.hostname.toLowerCase();
            if (this.config.cookieDomain) domains.add(String(this.config.cookieDomain).trim());
            if (hostname !== 'localhost' && !/^(\d{1,3}\.){3}\d{1,3}$/.test(hostname)) {
                const parts = hostname.split('.');
                for (let i = 0; i < parts.length - 1; i++) {
                    domains.add('.' + parts.slice(i).join('.'));
                }
            }
            return Array.from(domains).filter(Boolean);
        }

        clearLegacyDomainCookies (name) {
            this.getLegacyCookieDomains().forEach((domain) => {
                document.cookie = name + '=;domain=' + domain + ';path=/;max-age=0;SameSite=Lax';
            });
        }

        deleteCookie (name) {
            var cookie = name + '=;path=/;max-age=0;SameSite=Lax';
            document.cookie = cookie;
            this.clearLegacyDomainCookies(name);
        }

        async init () {
            try {
                await this.fetchWidgetSettings();
                if (!document.body) {
                    await new Promise((resolve) => {
                        document.addEventListener('DOMContentLoaded', resolve, { once: true });
                    });
                }
                this.createElements();
                this.setLauncherPosition();
                this.widgetButtonWrapper.style.display = 'none';
                this.iframe.addEventListener('load', () => {
                    this.channelReady = false;
                    this._pendingMessages = [];
                    this.startChannelHandshake();
                });
                this.setupMobileDetection();
                this.setupEventListeners();
                this.startPageTracking();
            } catch (error) {
                console.error('Failed to initialize Libredesk Widget:', error);
            }
        }

        async fetchWidgetSettings () {
            try {
                const settingsURL = new URL(`${this.baseURL}/api/v1/widget/chat/settings/launcher`);
                settingsURL.searchParams.set('inbox_id', this.config.inboxID);
                settingsURL.searchParams.set('parent_origin', window.location.origin);
                const response = await fetch(settingsURL.href, {
                    credentials: 'omit',
                    referrerPolicy: 'no-referrer'
                });

                if (!response.ok) {
                    throw new Error(`Error fetching widget settings. Status: ${response.status}`);
                }

                const result = await response.json();

                if (result.status !== 'success') {
                    throw new Error('Failed to fetch widget settings');
                }

                this.widgetSettings = result.data;
            } catch (error) {
                console.error('Error fetching widget settings:', error);
                throw error;
            }
        }

        contrastColor (hex) {
            try {
                hex = hex.replace(/^#/, '');
                var r = parseInt(hex.substring(0, 2), 16) / 255;
                var g = parseInt(hex.substring(2, 4), 16) / 255;
                var b = parseInt(hex.substring(4, 6), 16) / 255;
                var L = 0.2126 * r + 0.7152 * g + 0.0722 * b;
                return L > 0.179 ? '#000000' : '#ffffff';
            } catch (e) {
                return '#ffffff';
            }
        }

        safeHTTPURL (value) {
            if (!value) return '';
            try {
                const parsed = new URL(value, window.location.href);
                if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return '';
                if (parsed.username || parsed.password) return '';
                return parsed.href;
            } catch (_) {
                return '';
            }
        }

        createElements () {
            const launcher = this.widgetSettings.launcher;
            const colors = this.widgetSettings.colors;

            this.toggleButton = document.createElement('div');
            this.toggleButton.style.cssText = `
                position: fixed;
                cursor: pointer;
                z-index: 9999;
                width: ${this.isMobile ? this.MOBILE_LAUNCHER_SIZE : this.LAUNCHER_SIZE}px;
                height: ${this.isMobile ? this.MOBILE_LAUNCHER_SIZE : this.LAUNCHER_SIZE}px;
                background-color: ${launcher.color || colors.primary};
                border-radius: 50%;
                display: flex;
                justify-content: center;
                align-items: center;
                box-shadow: 0 8px 24px rgba(0,0,0,0.35), 0 2px 8px rgba(0,0,0,0.25);
                transition: transform 0.3s ease;
            `;

            this.iconContainer = document.createElement('div');
            this.iconContainer.style.cssText = `
                width: 100%;
                height: 100%;
                display: flex;
                justify-content: center;
                align-items: center;
                transition: transform 0.3s ease;
            `;

            this.defaultIcon = document.createElement('img');
            this.defaultIcon.src = this.safeHTTPURL(launcher.logo_url) || DEFAULT_LAUNCHER_LOGO;
            this.defaultIcon.style.cssText = `
                width: 100%;
                height: 100%;
                border-radius: 50%;
                object-fit: cover;
            `;
            this.iconContainer.appendChild(this.defaultIcon);

            this.arrowIcon = document.createElement('div');
            const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
            svg.setAttribute('width', '24');
            svg.setAttribute('height', '24');
            svg.setAttribute('viewBox', '0 0 24 24');
            svg.setAttribute('fill', 'none');
            const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
            path.setAttribute('d', 'M7 10L12 15L17 10');
            path.setAttribute('stroke', this.contrastColor(launcher.color || colors.primary));
            path.setAttribute('stroke-width', '2');
            path.setAttribute('stroke-linecap', 'round');
            path.setAttribute('stroke-linejoin', 'round');
            svg.appendChild(path);
            this.arrowIcon.appendChild(svg);
            this.arrowIcon.style.cssText = `
                width: 100%;
                height: 100%;
                display: none;
                justify-content: center;
                align-items: center;
            `;
            this.iconContainer.appendChild(this.arrowIcon);

            this.toggleButton.appendChild(this.iconContainer);

            this.unreadBadge = document.createElement('div');
            this.unreadBadge.style.cssText = `
                position: absolute;
                top: -5px;
                right: -5px;
                background-color: #ef4444;
                color: white;
                border-radius: 50%;
                width: 20px;
                height: 20px;
                display: none;
                justify-content: center;
                align-items: center;
                font-size: 12px;
                font-weight: bold;
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                border: 2px solid white;
                box-sizing: border-box;
                z-index: 10000;
            `;

            const widgetButtonWrapper = document.createElement('div');
            widgetButtonWrapper.style.cssText = `
                position: fixed;
                z-index: 9999;
            `;

            widgetButtonWrapper.appendChild(this.toggleButton);
            widgetButtonWrapper.appendChild(this.unreadBadge);
            this.toggleButton.style.position = 'relative';
            this.widgetButtonWrapper = widgetButtonWrapper;

            const reducedMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
            const iframeTransition = reducedMotion
                ? 'none'
                : 'width 0.3s ease, height 0.3s ease, bottom 0.3s ease, border-radius 0.3s ease, box-shadow 0.3s ease';

            this.iframe = document.createElement('iframe');
            const iframeURL = new URL(`${this.baseURL}/widget`);
            iframeURL.searchParams.set('inbox_id', this.config.inboxID);
            iframeURL.searchParams.set('parent_origin', window.location.origin);
            iframeURL.hash = 'ld_channel=' + encodeURIComponent(this.channelNonce);
            this.iframe.src = iframeURL.href;
            this.iframe.referrerPolicy = 'strict-origin';
            this.iframe.setAttribute('sandbox', 'allow-scripts allow-forms allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-downloads');
            this.iframe.style.cssText = `
                position: fixed;
                border: none;
                border-radius: ${this.IFRAME_BORDER_RADIUS};
                box-shadow: ${this.IFRAME_BOX_SHADOW};
                z-index: 9999;
                width: ${this.IFRAME_WIDTH};
                height: ${this.IFRAME_HEIGHT};
                transition: ${iframeTransition};
                display: none;
            `;

            document.body.appendChild(this.widgetButtonWrapper);
            document.body.appendChild(this.iframe);
        }

        sendMobileState () {
            this.isMobile = window.innerWidth <= this.MOBILE_BREAKPOINT;
            this.updateLauncherSize();
            this.postToIframe({
                type: 'SET_MOBILE_STATE',
                isMobile: this.isMobile
            });
        }

        updateLauncherSize () {
            if (!this.toggleButton) return;
            const size = this.isMobile ? this.MOBILE_LAUNCHER_SIZE : this.LAUNCHER_SIZE;
            this.toggleButton.style.width = size + 'px';
            this.toggleButton.style.height = size + 'px';
        }

        getNormalIframeHeight () {
            const bottom = this.widgetSettings.launcher.spacing.bottom;
            return `min(${this.IFRAME_HEIGHT}, calc(100vh - ${bottom + 100}px))`;
        }

        sendPageInfo () {
            const url = this.getSanitizedPageURL();
            if (!url) return;
            this.postToIframe({
                type: 'PAGE_VISIT',
                url: url,
                title: ''
            });
        }

        getSanitizedPageURL () {
            try {
                const current = new URL(window.location.href);
                if (current.protocol !== 'http:' && current.protocol !== 'https:') return '';
                // Paths, titles, queries, and fragments all commonly contain
                // account identifiers or one-time secrets. The default signal
                // is intentionally limited to the embedding origin.
                return new URL('/', current.origin).href;
            } catch (_) {
                return '';
            }
        }

        setLauncherPosition () {
            const spacing = this.widgetSettings.launcher.spacing;
            const side = this.widgetSettings.launcher.position === 'right' ? 'right' : 'left';
            this.widgetButtonWrapper.style.bottom = `${spacing.bottom}px`;
            this.widgetButtonWrapper.style[side] = `${spacing.side}px`;
        }

        applyIframeLayout () {
            if (!this.iframe) return;
            const iframe = this.iframe;

            if (this.isMobile) {
                iframe.style.top = '0';
                iframe.style.left = '0';
                iframe.style.right = '0';
                iframe.style.bottom = '0';
                iframe.style.width = '100vw';
                iframe.style.height = '100dvh';
                iframe.style.borderRadius = '0';
                iframe.style.boxShadow = 'none';
                return;
            }

            const spacing = this.widgetSettings.launcher.spacing;
            const side = this.widgetSettings.launcher.position === 'right' ? 'right' : 'left';

            iframe.style.top = '';
            iframe.style.left = '';
            iframe.style.right = '';
            iframe.style.borderRadius = this.IFRAME_BORDER_RADIUS;
            iframe.style.boxShadow = this.IFRAME_BOX_SHADOW;
            iframe.style[side] = `${spacing.side}px`;

            if (this.isExpanded) {
                iframe.style.width = this.EXPANDED_WIDTH;
                iframe.style.height = 'calc(100vh - 40px)';
                iframe.style.bottom = '20px';
            } else {
                iframe.style.width = this.IFRAME_WIDTH;
                iframe.style.height = this.getNormalIframeHeight();
                iframe.style.bottom = `${spacing.bottom + 80}px`;
            }
        }

        updateLauncherVisibility () {
            if (!this.widgetButtonWrapper) return;
            const shouldShow = this.widgetLoaded
                && !this.hideLauncher
                && !(this.isChatVisible && this.isMobile);
            this.widgetButtonWrapper.style.display = shouldShow ? '' : 'none';
        }

        handleMessage (event) {
            if (!this.iframe || event.source !== this.iframe.contentWindow) return;
            if (event.origin !== this.widgetOrigin || !this.isValidIframeMessage(event.data)) return;
            if (event.data.channelNonce !== this.channelNonce) return;

            switch (event.data.type) {
                case 'WIDGET_CHANNEL_READY':
                    this.channelReady = true;
                    this.stopChannelHandshake();
                    this.flushPendingMessages();
                    this.handleVueAppReady();
                    break;
                case 'CLOSE_WIDGET':
                    this.hideChat();
                    break;
                case 'UPDATE_UNREAD_COUNT':
                    this.updateUnreadCount(event.data.count);
                    break;
                case 'WIDGET_LOADED':
                    this.handleWidgetLoaded();
                    break;
                case 'EXPAND_WIDGET':
                    this.expandWidget();
                    break;
                case 'COLLAPSE_WIDGET':
                    this.collapseWidget();
                    break;
                case 'REQUEST_PAGE_INFO':
                    this.sendPageInfo();
                    break;
                case 'CLEAR_VISITOR_TOKEN':
                    this.deleteCookie(this.getCookieName('visitor'));
                    break;
                case 'CLEAR_SESSION_TOKEN':
                    this.deleteCookie(this.getCookieName('session'));
                    break;
                case 'SESSION_CLEARED':
                    this.deleteCookie(this.getCookieName('session'));
                    this.deleteCookie(this.getCookieName('visitor'));
                    this.settleLogout(true);
                    break;
                case 'SESSION_CLEAR_FAILED':
                    console.error('Libredesk logout failed; the session was not revoked.');
                    this.settleLogout(false);
                    break;
            }
        }

        isValidIframeMessage (data) {
            if (!data || typeof data !== 'object' || Array.isArray(data)) return false;
            if (data.bridge !== 'libredesk-widget-bridge' || data.version !== 1) return false;
            if (typeof data.channelNonce !== 'string' || data.channelNonce.length > 128) return false;
            const keys = Object.keys(data);
            const only = (allowed) => keys.every((key) => allowed.indexOf(key) !== -1);
            const base = ['bridge', 'version', 'channelNonce', 'type'];
            switch (data.type) {
                case 'WIDGET_CHANNEL_READY':
                case 'CLOSE_WIDGET':
                case 'WIDGET_LOADED':
                case 'EXPAND_WIDGET':
                case 'COLLAPSE_WIDGET':
                case 'REQUEST_PAGE_INFO':
                case 'CLEAR_VISITOR_TOKEN':
                case 'CLEAR_SESSION_TOKEN':
                case 'SESSION_CLEARED':
                case 'SESSION_CLEAR_FAILED':
                    return only(base);
                case 'UPDATE_UNREAD_COUNT':
                    return only(base.concat('count')) && Number.isSafeInteger(data.count) && data.count >= 0 && data.count <= 999999;
                default:
                    return false;
            }
        }

        setupEventListeners () {
            this.toggleButton.addEventListener('click', () => this.toggle());
            window.addEventListener('message', this._boundHandleMessage);
        }

        handleResize () {
            const wasMobile = this.isMobile;
            this.sendMobileState();
            if (this.isChatVisible && wasMobile !== this.isMobile) {
                this.applyIframeLayout();
                this.updateLauncherVisibility();
            }
        }

        setupMobileDetection () {
            window.addEventListener('resize', this._boundHandleResize);
            window.addEventListener('orientationchange', this._boundHandleResize);
        }

        handleVueAppReady () {
            this.sendMobileState();

            // Legacy parent-domain cookies are untrusted and all tokens from
            // that generation are invalid. Delete them without ever reading or
            // forwarding their values into the isolated widget frame.
            this.deleteCookie(this.getCookieName('session'));
            this.deleteCookie(this.getCookieName('visitor'));

            if (this.config.userJWT) {
                this.postToIframe({
                    type: 'SET_JWT_TOKEN',
                    jwt: this.config.userJWT
                });
                return;
            }

            this.postToIframe({ type: 'SESSION_DATA' });
        }

        handleWidgetLoaded () {
            this.widgetLoaded = true;
            this.updateLauncherVisibility();
        }

        toggle () {
            if (this.isChatVisible) {
                this.hideChat();
            } else {
                this.showChat();
            }
        }

        showChat () {
            if (!this.iframe) return;

            this.isMobile = window.innerWidth <= this.MOBILE_BREAKPOINT;
            this.isChatVisible = true;

            this.iframe.style.display = 'block';
            this.applyIframeLayout();
            this.updateLauncherVisibility();

            this.toggleButton.style.transform = 'scale(0.9)';
            this.unreadBadge.style.display = 'none';

            if (this.defaultIcon) this.defaultIcon.style.display = 'none';
            this.arrowIcon.style.display = 'flex';

            this.postToIframe({ type: 'WIDGET_OPENED' });

            if (this._onShowCallback) this._onShowCallback();
        }

        hideChat () {
            if (!this.iframe) return;

            this.iframe.style.display = 'none';
            this.isChatVisible = false;
            this.toggleButton.style.transform = 'scale(1)';
            this.updateLauncherVisibility();

            if (this.defaultIcon) this.defaultIcon.style.display = 'block';
            this.arrowIcon.style.display = 'none';

            if (this.unreadCount > 0) {
                this.unreadBadge.textContent = this.formatBadgeCount(this.unreadCount);
                this.unreadBadge.style.display = 'flex';
            }

            this.postToIframe({ type: 'WIDGET_CLOSED' });

            if (this._onHideCallback) this._onHideCallback();
        }

        updateUnreadCount (count) {
            this.unreadCount = count;
            if (this._onUnreadCountChangeCallback) this._onUnreadCountChangeCallback(count);

            if (count > 0 && !this.isChatVisible) {
                this.unreadBadge.textContent = this.formatBadgeCount(count);
                this.unreadBadge.style.display = 'flex';
            } else {
                this.unreadBadge.style.display = 'none';
            }
        }

        expandWidget () {
            if (!this.iframe || !this.isChatVisible || this.isMobile) return;
            this.isExpanded = true;
            this.applyIframeLayout();
            this.postToIframe({ type: 'WIDGET_EXPANDED', isExpanded: true });
        }

        collapseWidget () {
            if (!this.iframe || !this.isChatVisible || this.isMobile) return;
            this.isExpanded = false;
            this.applyIframeLayout();
            this.postToIframe({ type: 'WIDGET_EXPANDED', isExpanded: false });
        }

        startPageTracking () {
            this._lastPageURL = '';
            this._origPushState = history.pushState;
            this._origReplaceState = history.replaceState;

            const self = this;
            const onPageChange = () => {
                const url = self.getSanitizedPageURL();
                if (!url) return;
                if (url === self._lastPageURL) return;
                self._lastPageURL = url;
                // Defer to let SPA frameworks update document.title after route change.
                setTimeout(() => { self.sendPageInfo(); }, 100);
            };

            history.pushState = function () {
                self._origPushState.apply(this, arguments);
                onPageChange();
            };
            history.replaceState = function () {
                self._origReplaceState.apply(this, arguments);
                onPageChange();
            };

            this._onPopState = onPageChange;
            this._onHashChange = onPageChange;
            window.addEventListener('popstate', this._onPopState);
            window.addEventListener('hashchange', this._onHashChange);

            this._pageTrackInterval = setInterval(onPageChange, 7000);
            onPageChange();
        }

        stopPageTracking () {
            if (this._origPushState) history.pushState = this._origPushState;
            if (this._origReplaceState) history.replaceState = this._origReplaceState;
            if (this._onPopState) window.removeEventListener('popstate', this._onPopState);
            if (this._onHashChange) window.removeEventListener('hashchange', this._onHashChange);
            if (this._pageTrackInterval) clearInterval(this._pageTrackInterval);
        }

        setUser (jwt) {
            this.postToIframe({ type: 'SET_JWT_TOKEN', jwt: jwt });
        }

        logout () {
            if (this._logoutRequest) return this._logoutRequest.promise;

            var resolveRequest;
            var promise = new Promise((resolve) => { resolveRequest = resolve; });
            var timer = setTimeout(() => {
                console.error('Libredesk logout timed out; the session may still be active.');
                this.settleLogout(false);
            }, 10000);
            this._logoutRequest = { promise: promise, resolve: resolveRequest, timer: timer };
            this.postToIframe({ type: 'CLEAR_SESSION' });
            return promise;
        }

        settleLogout (success) {
            if (!this._logoutRequest) return;
            var request = this._logoutRequest;
            this._logoutRequest = null;
            clearTimeout(request.timer);
            request.resolve(success);
        }

        destroy () {
            this.stopPageTracking();
            this.stopChannelHandshake();
            this.settleLogout(false);
            window.removeEventListener('message', this._boundHandleMessage);
            window.removeEventListener('resize', this._boundHandleResize);
            window.removeEventListener('orientationchange', this._boundHandleResize);
            if (this.widgetButtonWrapper) {
                document.body.removeChild(this.widgetButtonWrapper);
                this.widgetButtonWrapper = null;
                this.toggleButton = null;
                this.unreadBadge = null;
            }
            if (this.iframe) {
                document.body.removeChild(this.iframe);
                this.iframe = null;
            }
            this.isChatVisible = false;
            this._onShowCallback = null;
            this._onHideCallback = null;
            this._onUnreadCountChangeCallback = null;
            this.channelReady = false;
            this._pendingMessages = [];
        }
    }

    Libredesk.prototype.show = Libredesk.prototype.showChat;
    Libredesk.prototype.hide = Libredesk.prototype.hideChat;
    Libredesk.prototype.isVisible = function () { return this.isChatVisible; };
    Libredesk.prototype.onShow = function (fn) { this._onShowCallback = fn; };
    Libredesk.prototype.onHide = function (fn) { this._onHideCallback = fn; };
    Libredesk.prototype.onUnreadCountChange = function (fn) { this._onUnreadCountChangeCallback = fn; fn(this.unreadCount); };

    window.Libredesk = Libredesk;

    window.initLibredesk = function (config = {}) {
        if (window.Libredesk && window.Libredesk instanceof Libredesk) {
            console.warn('Libredesk Widget is already initialized');
            return window.Libredesk;
        }
        window.Libredesk = new Libredesk(config);
        return window.Libredesk;
    };

    function autoInit () {
        if (window.LibredeskSettings) {
            window.initLibredesk(window.LibredeskSettings);
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', autoInit, { once: true });
    } else {
        autoInit();
    }

})();
