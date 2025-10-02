const CDN_PROVIDERS = {
    aliyun: {
        name: "阿里云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        typeSelect: [
            "CDN",
            "DCDN"
        ],
        idHelpHtml: "<a target='_blank' href='https://ram.console.aliyun.com/manage/ak?spm=5176.12818093.nav-right.dak.488716d0mHaMgg'>创建 AccessKey</a>",
        typeHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>CDN</a>、<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>CDN</a>",
    },
    baiducloud: {
        name: "百度智能云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        typeSelect: [
            "CDN",
            "DRCDN"
        ],
        idHelpHtml: "<a target='_blank' href='https://console.bce.baidu.com/iam/?_=1651763238057#/iam/accesslist'>创建 AccessKey </a>",
        typeHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>CDN</a>、<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>DRCDN</a>",
    }
}