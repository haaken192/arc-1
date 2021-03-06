/*
Copyright (c) 2018 HaakenLabs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package scene

import (
	"github.com/go-gl/gl/v4.3-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/haakenlabs/arc/core"
	"github.com/haakenlabs/arc/graphics"
	"github.com/haakenlabs/arc/system/asset/shader"
	"github.com/haakenlabs/arc/system/input"
	"github.com/haakenlabs/arc/system/instance"
	"github.com/haakenlabs/arc/system/window"
)

var _ GraphListener = &Camera{}
var _ ScriptComponent = &Camera{}

type RenderPath int

const (
	RenderPathForward RenderPath = iota
	RenderPathDeferred
)

type CameraTexture int

const (
	CameraTextureLDR0 CameraTexture = iota
	CameraTextureLDR1
	CameraTextureHDR0
	CameraTextureHDR1
	CameraTextureDepth
	CameraTextureNormals
)

type CameraShader int

const (
	CameraShaderCopy CameraShader = iota
	CameraShaderDeferred
	CameraShaderNormals
	CameraShaderSkybox
)

type CameraMesh int

const (
	CameraMeshEffect CameraMesh = iota
	CameraMeshGBuffer
	CameraMeshSkybox
)

type ClearMode int

const (
	ClearModeSkybox ClearMode = iota
	ClearModeColor
	ClearModeDepth
	ClearModeNothing
)

type Camera struct {
	BaseScriptComponent

	textures         map[CameraTexture]*graphics.Texture2D
	shaders          map[CameraShader]*graphics.Shader
	meshes           map[CameraMesh]*graphics.Mesh
	effects          []Effect
	deferredCache    []Drawable
	forwardCache     []Drawable
	framebuffer      *graphics.Framebuffer
	gbuffer          *graphics.GBuffer
	projectionMatrix mgl32.Mat4
	viewMatrix       mgl32.Mat4
	normalMatrix     mgl32.Mat3
	clearColor       core.Color
	clearMode        ClearMode
	renderPath       RenderPath
	activeRenderPath RenderPath
	aspectRatio      float32
	fov              float32
	nearClip         float32
	farClip          float32
	effectPass       int32
	effectActiveType EffectType
	hdr              bool
	orthographic     bool
}

func (c *Camera) SetClearMode(mode ClearMode) {
	c.clearMode = mode
}

func (c *Camera) Render() {
	c.startRender()

	c.renderDeferred()
	c.renderForward()
	//c.renderNormals()
	c.renderEffects()

	c.endRender()
}

func (c *Camera) startRender() {
	c.framebuffer.Bind()

	if c.hdr {
		c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT1})
	} else {
		c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT0})
	}

	c.clearBackground()
}

func (c *Camera) endRender() {
	graphics.UnbindCurrentFramebuffer()
	graphics.BlitFramebuffers(c.framebuffer, nil, gl.COLOR_ATTACHMENT0)
}

func (c *Camera) clearBackground() {
	if c.clearMode == ClearModeNothing {
		return
	}
	if c.clearMode == ClearModeDepth {
		c.framebuffer.ClearBufferFlags(gl.DEPTH_BUFFER_BIT)
		return
	}

	if c.clearMode == ClearModeColor {
		gl.ClearColor(c.clearColor.Elem())
		c.framebuffer.ClearBuffers()
		gl.ClearColor(0.0, 0.0, 0.0, 1.0)
	} else if c.clearMode == ClearModeSkybox {
		c.framebuffer.ClearBuffers()

		skybox := c.GameObject().Environment().Skybox
		if skybox == nil {
			return
		}

		c.meshes[CameraMeshSkybox].Bind()
		c.shaders[CameraShaderSkybox].Bind()
		skybox.Specular().ActivateTexture(gl.TEXTURE0)
		c.shaders[CameraShaderSkybox].SetUniform("v_view_matrix", c.ViewMatrix())
		c.shaders[CameraShaderSkybox].SetUniform("v_projection_matrix", c.ProjectionMatrix())
		c.meshes[CameraMeshSkybox].Draw()
		c.shaders[CameraShaderSkybox].Unbind()
		c.meshes[CameraMeshSkybox].Unbind()
	}
}

func (c *Camera) ProjectionMatrix() mgl32.Mat4 {
	return c.projectionMatrix
}

func (c *Camera) ViewMatrix() mgl32.Mat4 {
	return c.viewMatrix
}

func (c *Camera) NormalMatrix() mgl32.Mat3 {
	return c.normalMatrix
}

func (c *Camera) RenderPath() RenderPath {
	return c.renderPath
}

func (c *Camera) ActiveRenderPath() RenderPath {
	return c.activeRenderPath
}

func (c *Camera) SetProjectionMatrix(m mgl32.Mat4) {
	c.projectionMatrix = m
}

func (c *Camera) SetViewMatrix(m mgl32.Mat4) {
	c.viewMatrix = m
}

func (c *Camera) SetNormalMatrix(m mgl32.Mat3) {
	c.normalMatrix = m
}

func (c *Camera) UpdateMatrices() {
	if c.orthographic {
		c.SetProjectionMatrix(mgl32.Ortho2D(0, float32(window.Resolution().X()), float32(window.Resolution().Y()), 0))
	} else {
		c.SetProjectionMatrix(mgl32.Perspective(c.fov, c.aspectRatio, c.nearClip, c.farClip))
	}
}

func (c *Camera) AspectRatio() float32 {
	return c.aspectRatio
}

func (c *Camera) Fov() float32 {
	return c.fov
}

func (c *Camera) NearClip() float32 {
	return c.nearClip
}

func (c *Camera) FarClip() float32 {
	return c.farClip
}

func (c *Camera) SetFov(fov float32) {
	c.fov = fov
}

func (c *Camera) CameraPosition() mgl32.Vec3 {
	return c.GetTransform().Position()
}

func (c *Camera) Look() mgl32.Quat {
	return mgl32.Quat{}
}

func (c *Camera) LookDirection() mgl32.Vec3 {
	return mgl32.Vec3{}
}

func (c *Camera) HDR() bool {
	return c.hdr
}

func (c *Camera) AddEffect(effect Effect) {
	c.effects = append(c.effects, effect)
}

func (c *Camera) OnSceneGraphUpdate() {
	c.deferredCache = c.deferredCache[:0]
	c.forwardCache = c.forwardCache[:0]

	var drawables []Drawable

	components := c.GameObject().Scene().Components()
	for i := range components {
		if r, ok := components[i].(Drawable); ok {
			drawables = append(drawables, r)
		}
	}

	switch c.renderPath {
	case RenderPathForward:
		c.forwardCache = drawables
	case RenderPathDeferred:
		for i := range drawables {
			if drawables[i].SupportsDeferred() {
				c.deferredCache = append(c.deferredCache, drawables[i])
			} else {
				c.forwardCache = append(c.forwardCache, drawables[i])
			}
		}
	}
}

func (c *Camera) setupPipeline() {
	size := window.Resolution()

	c.framebuffer = graphics.NewFramebuffer(size)

	c.meshes[CameraMeshEffect] = graphics.NewMeshQuad()
	c.meshes[CameraMeshSkybox] = graphics.NewMeshQuadBack()

	c.shaders[CameraShaderCopy] = shader.NewShaderUtilsCopy()
	c.shaders[CameraShaderSkybox] = shader.NewShaderUtilsSkybox()
	// FIXME: Replace with real shader.
	c.shaders[CameraShaderNormals] = shader.NewShaderUtilsCopy()

	c.textures[CameraTextureLDR0] = graphics.NewTexture2D(size, graphics.TextureFormatDefaultColor)
	c.textures[CameraTextureLDR1] = graphics.NewTexture2D(size, graphics.TextureFormatDefaultColor)
	c.textures[CameraTextureDepth] = graphics.NewTexture2D(size, graphics.TextureFormatDefaultDepth)
	c.textures[CameraTextureNormals] = graphics.NewTexture2D(size, graphics.TextureFormatRGBA16)

	if c.hdr {
		c.textures[CameraTextureHDR0] = graphics.NewTexture2D(size, graphics.TextureFormatDefaultHDRColor)
		c.textures[CameraTextureHDR1] = graphics.NewTexture2D(size, graphics.TextureFormatDefaultHDRColor)
	}

	for k := range c.textures {
		c.textures[k].Alloc()
	}

	c.framebuffer.SetAttachment(gl.COLOR_ATTACHMENT0, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureLDR0], false))
	c.framebuffer.SetAttachment(gl.COLOR_ATTACHMENT2, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureLDR1], false))
	c.framebuffer.SetAttachment(gl.COLOR_ATTACHMENT4, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureNormals], false))
	c.framebuffer.SetAttachment(gl.DEPTH_ATTACHMENT, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureDepth], false))

	if c.hdr {
		c.framebuffer.SetAttachment(gl.COLOR_ATTACHMENT1, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureHDR0], false))
		c.framebuffer.SetAttachment(gl.COLOR_ATTACHMENT3, graphics.NewAttachmentTexture2DFrom(c.textures[CameraTextureHDR1], false))
	}

	if err := c.framebuffer.Alloc(); err != nil {
		panic(err)
	}

	if c.renderPath == RenderPathDeferred {
		c.meshes[CameraMeshGBuffer] = graphics.NewMeshQuad()
		// FIXME: Get from scene's environment settings.
		c.shaders[CameraShaderDeferred] = shader.DefaultShader()

		depthAttachment := c.framebuffer.GetAttachment(gl.DEPTH_ATTACHMENT).(*graphics.AttachmentTexture2D)
		c.gbuffer = graphics.NewGBuffer(size, depthAttachment, c.hdr)

		if err := c.gbuffer.Alloc(); err != nil {
			panic(err)
		}
	}
}

func (c *Camera) renderDeferred() {
	if c.renderPath != RenderPathDeferred {
		return
	}

	if c.shaders[CameraShaderDeferred] == nil {
		return
	}

	skybox := c.GameObject().Environment().Skybox

	c.activeRenderPath = RenderPathDeferred

	// Pass 1 : Geometry

	c.gbuffer.Bind()
	c.gbuffer.ClearBuffers()

	for i := range c.deferredCache {
		c.deferredCache[i].Draw(c)
	}
	c.gbuffer.Unbind()

	// Pass 2 : Ambient Lighting

	c.shaders[CameraShaderDeferred].Bind()
	c.shaders[CameraShaderDeferred].SetSubroutine(graphics.ShaderComponentFragment, "deferred_pass_ambient")
	c.shaders[CameraShaderDeferred].SetUniform("v_model_matrix", mgl32.Ident4())
	c.shaders[CameraShaderDeferred].SetUniform("v_view_matrix", mgl32.Ident4())
	c.shaders[CameraShaderDeferred].SetUniform("v_projection_matrix", mgl32.Ident4())
	c.shaders[CameraShaderDeferred].SetUniform("f_camera", c.GetTransform().Position())
	c.shaders[CameraShaderDeferred].SetUniform("f_dimensions", c.gbuffer.Size())

	gl.DepthMask(false)

	c.meshes[CameraMeshGBuffer].Bind()
	c.gbuffer.Attachment0().ActivateTexture(gl.TEXTURE0)
	c.gbuffer.Attachment1().ActivateTexture(gl.TEXTURE1)
	c.gbuffer.AttachmentDepth().ActivateTexture(gl.TEXTURE2)

	if skybox != nil {
		skybox.Specular().ActivateTexture(gl.TEXTURE3)
		skybox.Irradiance().ActivateTexture(gl.TEXTURE4)
	}

	c.meshes[CameraMeshGBuffer].Draw()

	c.meshes[CameraMeshGBuffer].Unbind()
	c.shaders[CameraShaderDeferred].Unbind()

	gl.DepthMask(true)
}

func (c *Camera) renderForward() {
	c.activeRenderPath = RenderPathForward

	// TODO: For each light?

	for i := range c.forwardCache {
		c.forwardCache[i].Draw(c)
	}
}

func (c *Camera) renderNormals() {
	c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT4})
	c.framebuffer.ClearBufferFlags(gl.COLOR_BUFFER_BIT)
	c.shaders[CameraShaderNormals].Bind()

	gl.DepthFunc(gl.LEQUAL)
	for i := range c.forwardCache {
		c.forwardCache[i].DrawShader(c.shaders[CameraShaderNormals], c)
	}
	for i := range c.deferredCache {
		c.deferredCache[i].DrawShader(c.shaders[CameraShaderNormals], c)
	}
	gl.DepthFunc(gl.LESS)

	c.shaders[CameraShaderNormals].Unbind()
}

func (c *Camera) renderEffects() {
	if len(c.effects) == 0 {
		return
	}

	gl.DepthMask(false)
	gl.Disable(gl.DEPTH_TEST)

	if c.hdr {
		c.effectActiveType = EffectTypeHDR

		for i := range c.effects {
			if c.effects[i].Type() == EffectTypeTonemapper {
				c.effectActiveType = EffectTypeTonemapper

				c.startEffectPass()
				c.effects[i].Render(c)
				c.endEffectPass()

				c.effectActiveType = EffectTypeLDR

				continue
			}

			c.startEffectPass()
			c.effects[i].Render(c)
			c.endEffectPass()
		}
	} else {
		c.effectActiveType = EffectTypeLDR
		for i := range c.effects {
			c.startEffectPass()
			c.effects[i].Render(c)
			c.endEffectPass()
		}
	}

	gl.Enable(gl.DEPTH_TEST)
	gl.DepthMask(true)
}

func (c *Camera) EffectPass() {
	if c.effectActiveType == EffectTypeHDR {
		if c.effectPass%2 == 1 {
			c.textures[CameraTextureHDR1].ActivateTexture(gl.TEXTURE0)
			c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT1})

		} else {
			c.textures[CameraTextureHDR0].ActivateTexture(gl.TEXTURE0)
			c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT3})
		}
	} else if c.effectActiveType == EffectTypeLDR {
		if c.effectPass%2 == 1 {
			c.textures[CameraTextureLDR1].ActivateTexture(gl.TEXTURE0)
			c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT0})
		} else {
			c.textures[CameraTextureLDR0].ActivateTexture(gl.TEXTURE0)
			c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT2})
		}
	}

	c.meshes[CameraMeshEffect].Bind()
	c.meshes[CameraMeshEffect].Draw()
	c.meshes[CameraMeshEffect].Unbind()

	c.effectPass++
}

func (c *Camera) startEffectPass() {
	c.effectPass = 0

	if c.effectActiveType == EffectTypeTonemapper {
		c.textures[CameraTextureHDR0].ActivateTexture(gl.TEXTURE0)
		c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT0})
	}
}

func (c *Camera) endEffectPass() {
	if c.hdr {
		c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT1})
	} else {
		c.framebuffer.ApplyDrawBuffers([]uint32{gl.COLOR_ATTACHMENT0})
	}

	if c.effectActiveType == EffectTypeTonemapper {
		return
	}

	if c.effectPass%2 == 1 {
		return
	}

	c.shaders[CameraShaderCopy].Bind()
	c.shaders[CameraShaderCopy].SetSubroutine(graphics.ShaderComponentFragment, "pass_0")

	if c.effectActiveType == EffectTypeHDR {
		c.textures[CameraTextureHDR1].ActivateTexture(gl.TEXTURE0)
	} else {
		c.textures[CameraTextureLDR1].ActivateTexture(gl.TEXTURE0)
	}

	c.meshes[CameraMeshEffect].Bind()
	c.meshes[CameraMeshEffect].Draw()
	c.meshes[CameraMeshEffect].Unbind()
	c.shaders[CameraShaderCopy].Unbind()
}

func NewCamera(renderPath RenderPath, hdr bool) *Camera {
	c := &Camera{
		hdr:           hdr,
		renderPath:    renderPath,
		meshes:        make(map[CameraMesh]*graphics.Mesh),
		shaders:       make(map[CameraShader]*graphics.Shader),
		textures:      make(map[CameraTexture]*graphics.Texture2D),
		effects:       []Effect{},
		deferredCache: []Drawable{},
		forwardCache:  []Drawable{},
		fov:           1.309,
		nearClip:      0.01,
		farClip:       100000.0,
		aspectRatio:   window.AspectRatio(),
		clearColor:    core.ColorBlack,
	}

	c.SetName("Camera")
	instance.MustAssign(c)

	c.setupPipeline()
	c.UpdateMatrices()

	return c
}

func CameraComponent(g *GameObject) *Camera {
	c := g.Components()
	for i := range c {
		if ct, ok := c[i].(*Camera); ok {
			return ct
		}
	}

	return nil
}

func (c *Camera) Awake() {
	c.Resize()
}

func (c *Camera) Update() {
	if input.WindowResized() {
		c.Resize()
	}
}

func (c *Camera) Resize() {
	c.aspectRatio = window.AspectRatio()
	c.framebuffer.SetSize(window.Resolution())
	if c.renderPath == RenderPathDeferred {
		c.gbuffer.SetSize(window.Resolution())
	}
	c.UpdateMatrices()
}
