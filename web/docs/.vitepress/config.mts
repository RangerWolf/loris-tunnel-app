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
            { text: 'Learn More', link: '/articles/introduction' },
            { text: 'OpenClaw + Tunnels', link: '/articles/openclaw-remote-gateway-ssh-tunnel' },
            { text: 'Articles', link: '/articles/' }
        ],

        sidebar: [
            {
                text: 'Introduction',
                items: [
                    { text: 'Overview', link: '/articles/introduction' },
                ]
            },
            {
                text: 'Articles',
                items: [
                    { text: 'All Articles', link: '/articles/' },
                    { text: 'OpenClaw + SSH Tunnels', link: '/articles/openclaw-remote-gateway-ssh-tunnel' },
                    { text: 'Sample Template', link: '/articles/sample' },
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
