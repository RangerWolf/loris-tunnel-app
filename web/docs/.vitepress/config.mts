import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
    title: "Loris Tunnel",
    description: "A desktop GUI application for managing SSH tunnels — with automatic reconnection and a clean interface.",
    head: [
        ['script', { async: '', src: 'https://www.googletagmanager.com/gtag/js?id=G-776FG9SDVQ' }],
        ['script', {}, `window.dataLayer = window.dataLayer || []; function gtag(){dataLayer.push(arguments);} gtag('js', new Date()); gtag('config', 'G-776FG9SDVQ');`],
        ['link', { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' }],
        ['link', { rel: 'shortcut icon', type: 'image/x-icon', href: '/favicon.ico' }]
    ],
    themeConfig: {
        logo: '../imgs/logo.png',
        // https://vitepress.dev/reference/default-theme-config
        nav: [
            { text: 'Home', link: '/' },
            { text: 'Learn More', link: '/articles/20260316-introduction' },
            { text: 'Articles', link: '/articles/' }
        ],

        sidebar: [
            {
                text: 'Introduction',
                items: [
                    { text: 'Overview', link: '/articles/20260316-introduction' },
                ]
            },
            {
                text: 'Articles',
                items: [
                    { text: 'All Articles', link: '/articles/' },
                    { text: 'Codex SSH Login on Remote Servers', link: '/articles/20260331-codex-ssh-login-with-loris-tunnel' },
                    { text: 'OpenClaw + SSH Tunnels', link: '/articles/20260329-openclaw-remote-gateway-ssh-tunnel' },
                ]
            }
        ],

        socialLinks: [
            { icon: 'github', link: 'https://github.com/RangerWolf/loris-tunnel-app/' }
        ],

        footer: {
            message: 'Released under the Apache 2.0 License. Contact: <a href="mailto:yang.rangerwolf@gmail.com">yang.rangerwolf@gmail.com</a>',
            copyright: 'Copyright © 2024-present'
        }
    }
})
