import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
    title: "Loris Tunnel",
    description: "A desktop GUI application for managing SSH tunnels — with automatic reconnection and a clean interface.",
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
                    { text: 'Cherry Studio, Codex & SSH MCP (SSH Pilot)', link: '/articles/20260405-cherry-studio-codex-ssh-mcp-pilot' },
                    { text: 'Codex SSH Login on Remote Servers', link: '/articles/20260331-codex-ssh-login-with-loris-tunnel' },
                    { text: 'OpenClaw + SSH Tunnels', link: '/articles/20260329-openclaw-remote-gateway-ssh-tunnel' },
                    { text: 'Sample Template', link: '/articles/20260316-sample' },
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
