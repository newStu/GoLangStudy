import { defineConfig } from 'vitepress'

// 部署到 GitHub Pages 项目站点时（https://<user>.github.io/<repo>/），
// base 需要设置为 '/<仓库名>/'，例如本仓库为 GoLangStudy
export default defineConfig({
  lang: 'zh-CN',
  title: 'GoLang 学习笔记',
  description: '记录 GoLang Web 项目学习过程中的笔记',

  base: '/GoLangStudy/',

  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '基础语法', link: '/basics/syntax' },
      { text: 'Web 项目', link: '/web/routing' }
    ],

    sidebar: {
      '/basics/': [
        {
          text: '基础语法',
          items: [
            { text: '基础语法', link: '/basics/syntax' }
          ]
        },
        {
          text: '并发编程',
          items: [
            { text: 'channel 的关闭与 select 结束时机', link: '/basics/channel的关闭与select结束时机' },
            { text: 'map 线程安全与 sync.Map', link: '/basics/map线程安全与syncMap' }
          ]
        }
      ],
      '/web/': [
        {
          text: 'Web 项目',
          items: [
            { text: '01 项目初始化与路由组织', link: '/web/01-项目初始化与路由组织' },
            { text: '02 登录接口与分层架构', link: '/web/02-登录接口与分层架构' },
            { text: '03 数据库接入与 GORM', link: '/web/03-数据库接入与GORM' },
            { text: '项目结构（待补充）', link: '/web/structure' },
            { text: '路由与 HTTP（待补充）', link: '/web/routing' }
          ]
        }
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/gc_wangzhaoyu_ex/GoLangStudy' }
    ]
  }
})
