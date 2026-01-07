const CDN_PROVIDERS = {
    aliyun: {
        name: "阿里云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        typeSelect: [
            "ESA",
            "CDN",
            "DCDN",
        ],
        idHelpHtml: "<a target='_blank' href='https://ram.console.aliyun.com/manage/ak?spm=5176.12818093.nav-right.dak.488716d0mHaMgg'>创建 AccessKey</a>",
        typeHelpHtml: "<a target='_blank' href='https://esa.console.aliyun.com'>ESA</a>、<a target='_blank' href='https://cdn.console.aliyun.com/overview'>CDN</a>、<a target='_blank' href='https://dcdn.console.aliyun.com/#/overview'>DCDN</a>",
        maxSources: [1, 20, 5],
        protocolTipHtml: [
            "<tip>阿里云 ESA 类型不支持自定义端口</tip>",
            "<tip>阿里云 CDN 类型不支持自定义 HTTPS 端口</tip>",
            "<tip>阿里云 DCDN 类型不支持自定义 HTTPS 端口</tip"
        ]
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
        typeHelpHtml: "<a target='_blank' href='https://console.bce.baidu.com/cdn#/cdn/list'>CDN</a>、<a target='_blank' href='https://console.bce.baidu.com/cdn#/cdn/list'>DRCDN</a>",
        maxSources: [5, 5],
        protocolTipHtml: [
            "<tip></tip>",
            "<tip></tip>",
            "<tip></tip>"
        ]
    },
}
