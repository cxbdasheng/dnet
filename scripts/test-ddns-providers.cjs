const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const html = fs.readFileSync(path.join(__dirname, '../web/ddns.html'), 'utf8');
const providers = fs.readFileSync(path.join(__dirname, '../static/dns.js'), 'utf8');
const update = html.match(/        function updateProviderRecordTypes\(providerId\) \{[\s\S]*?\n        \}/)[0];

function setup() {
    const nodes = ['A', 'AAAA', 'CNAME', 'TXT'].map(value => ({ value, checked: false, disabled: false }));
    let visible = [];
    const context = vm.createContext({
        $(target) {
            if (typeof target === 'string') return { each(fn) { nodes.forEach(node => fn.call(node)); } };
            return { prop(key, value) { target[key] = value; return this; } };
        },
        form: { render() {} },
        getSelectedRecordTypes: () => nodes.filter(n => n.checked).map(n => n.value),
        updateRecordTypeFields: types => { visible = Array.from(types); },
    });
    vm.runInContext(providers + '\n' + update, context);
    return { nodes, update: id => context.updateProviderRecordTypes(id), visible: () => visible };
}

for (const provider of ['dnsla', 'porkbun']) {
    test(provider + ' preserves CNAME and TXT when switching providers', () => {
        const s = setup();
        s.nodes.find(n => n.value === 'CNAME').checked = true;
        s.update(provider);
        assert.ok(s.nodes.every(n => !n.disabled));
        assert.deepEqual(s.visible(), ['CNAME']);
        s.nodes.find(n => n.value === 'CNAME').checked = false;
        s.nodes.find(n => n.value === 'TXT').checked = true;
        s.nodes.find(n => n.value === 'A').checked = true;
        s.nodes.find(n => n.value === 'AAAA').checked = true;
        s.update(provider);
        assert.deepEqual(s.visible(), ['A', 'AAAA', 'TXT']);
        s.update('alidns');
        assert.ok(s.nodes.every(n => !n.disabled));
        assert.deepEqual(s.visible(), ['A', 'AAAA', 'TXT']);
    });
}

test('DDNS inline scripts remain valid JavaScript', () => {
    for (const match of html.matchAll(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/g)) {
        new vm.Script(match[1].replace(/{{[\s\S]*?}}/g, 'null'));
    }
});
