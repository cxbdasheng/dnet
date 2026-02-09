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
    tencent:{
        name: "腾讯云",
        idLabel: "SecretId：",
        secretLabel: "SecretKey：",
        idHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>创建腾讯云 API 密钥</a>",
    }
}
