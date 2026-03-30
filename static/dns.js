const DNS_PROVIDERS = {
    alidns: {
        name: "阿里云",
        idLabel: "AccessKey ID",
        secretLabel: "AccessKey Secret",
        idHelpHtml: "<a target='_blank' href='https://ram.console.aliyun.com/manage/ak?spm=5176.12818093.nav-right.dak.488716d0mHaMgg'>创建 AccessKey</a>",
    },
    baiducloud: {
        name: "百度智能云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        idHelpHtml: "<a target='_blank' href='https://console.bce.baidu.com/iam/?_=1651763238057#/iam/accesslist'>创建 AccessKey</a>",
    },
    tencent: {
        name: "腾讯云",
        idLabel: "SecretId：",
        secretLabel: "SecretKey：",
        idHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>创建腾讯云 API 密钥</a>",
    },
    cloudflare: {
        name: "Cloudflare",
        idLabel: "API Token：",
        secretLabel: "",
        idHelpHtml: "<a target='_blank' href='https://dash.cloudflare.com/profile/api-tokens'>创建 API 令牌 -> 编辑区域 DNS (使用模板)</a>",
    },
    huawei: {
        name: "华为云",
        idLabel: "Access Key ID：",
        secretLabel: "Secret Access Key：",
        idHelpHtml: "<a target='_blank' href='https://console.huaweicloud.com/iam/#/mine/accessKey'>创建访问密钥</a>",
    },
    dnspod: {
        name: "DNSPod",
        idLabel: "Token ID：",
        secretLabel: "Token：",
        idHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/token'>创建 DNSPod Token</a>",
    },
    namesilo: {
        name: "NameSilo",
        idLabel: "",
        secretLabel: "API Key：",
        idHelpHtml: "<a target='_blank' href='https://www.namesilo.com/account/api-manager'>获取 NameSilo API Key</a>",
    },
}
