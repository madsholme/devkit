// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import VMConnections from '../src/components/VMConnections.vue'
import type { VM } from '../src/types'
vi.mock('../src/components/CopyButton.vue', () => ({ default: { props: ['text', 'label'], template: '<button :aria-label="label" :data-copy="text">Copy</button>' } }))
const vm = (connections: VM['connections']) => ({ id: 'demo', name: 'demo-vm', connections } as VM)
const spice = { kind: 'spice', port: 5931, listen: '192.168.1.20', tunnel: 'ssh tunnel', command: 'remote-viewer spice://127.0.0.1:5931', direct: [{ host: '192.168.1.20', uri: 'spice://192.168.1.20:5931', command: "remote-viewer 'spice://192.168.1.20:5931'" }] }
it('defaults to SPICE and separates direct details from numbered tunnel steps', async () => {
 const view = mount(VMConnections, { props: { vm: vm([{kind:'vnc', command:'vnc command', direct_note:'Loopback only'}, spice]) } })
 expect(view.get('summary').text()).toContain('demo-vm')
 expect(view.get('h3').text()).toBe('SPICE')
 const tunnel = view.get('[aria-label="SSH tunnel"]')
 const direct = view.get('[aria-label="Direct connection"]')
 expect(tunnel.text()).toContain('1. Open the tunnel')
 expect(tunnel.text()).toContain('2. Connect to the VM')
 expect(direct.text()).toContain('192.168.1.20')
 expect(direct.text()).toContain('5931')
 expect(direct.text()).toContain('spice://192.168.1.20:5931')
 expect(direct.get('[data-copy]').attributes('data-copy')).toBe(spice.direct[0].command)
 await view.get('select').setValue('vnc:0:')
 expect(view.get('h3').text()).toBe('VNC')
 expect(view.text()).toContain('Loopback only')
 expect(view.text()).not.toContain('192.168.1.20')
 await view.setProps({ vm: vm([spice]) })
 expect(view.get('h3').text()).toBe('SPICE')
})
it('keeps direct access visible without an available host SSH command', () => {
 const view=mount(VMConnections,{props:{vm:vm([{...spice,tunnel:undefined,command:undefined}])}})
 expect(view.get('[aria-label="Direct connection"]').text()).toContain('192.168.1.20')
 expect(view.get('[aria-label="SSH tunnel"]').text()).toContain('Host SSH details are unavailable')
})
it('explains loopback-only access and preserves the serial console flow', () => {
 const view=mount(VMConnections,{props:{vm:vm([{kind:'ssh',tunnel:'ssh tunnel',command:'ssh guest',direct_note:'This listener is bound to loopback.'}])}})
 expect(view.get('[aria-label="Direct connection"]').text()).toContain('loopback')
 const consoleView=mount(VMConnections,{props:{vm:vm([{kind:'console',command:'ssh -t host virsh console uuid'}])}})
 expect(consoleView.text()).toContain('Serial console')
 expect(consoleView.text()).toContain('Ctrl+]')
 expect(consoleView.find('[aria-label="Direct connection"]').exists()).toBe(false)
})
