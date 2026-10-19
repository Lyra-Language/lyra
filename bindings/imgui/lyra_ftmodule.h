/* The FreeType modules bindings.imgui builds (build.sh), in place of FreeType's own list
   (include/freetype/config/ftmodule.h): TrueType and OpenType — CFF outlines and variable
   fonts included — hinted natively or by the auto-hinter, drawn by the smooth rasterizer.
   Type 1, CID, bitmap formats, SDF and OT-SVG are left out. */
FT_USE_MODULE( FT_Module_Class, autofit_module_class )
FT_USE_MODULE( FT_Driver_ClassRec, tt_driver_class )
FT_USE_MODULE( FT_Driver_ClassRec, cff_driver_class )
FT_USE_MODULE( FT_Module_Class, psaux_module_class )
FT_USE_MODULE( FT_Module_Class, psnames_module_class )
FT_USE_MODULE( FT_Module_Class, pshinter_module_class )
FT_USE_MODULE( FT_Module_Class, sfnt_module_class )
FT_USE_MODULE( FT_Renderer_Class, ft_smooth_renderer_class )
