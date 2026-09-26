(()=>{var eS=Object.create;var hm=Object.defineProperty;var tS=Object.getOwnPropertyDescriptor;var nS=Object.getOwnPropertyNames;var iS=Object.getPrototypeOf,rS=Object.prototype.hasOwnProperty;var dn=(i,e)=>()=>{try{return e||i((e={exports:{}}).exports,e),e.exports}catch(n){throw e=0,n}};var sS=(i,e,n,r)=>{if(e&&typeof e=="object"||typeof e=="function")for(let s of nS(e))!rS.call(i,s)&&s!==n&&hm(i,s,{get:()=>e[s],enumerable:!(r=tS(e,s))||r.enumerable});return i};var oS=(i,e,n)=>(n=i!=null?eS(iS(i)):{},sS(e||!i||!i.__esModule?hm(n,"default",{value:i,enumerable:!0}):n,i));var bd=dn((fB,S_)=>{S_.exports=function(e){return e===0?"x":e===1?"y":e===2?"z":"c"+(e+1)}});var Vr=dn((dB,w_)=>{var xT=bd();w_.exports=function(e){return n;function n(r,s){let o=s&&s.indent||0,a=s&&s.join!==void 0?s.join:`
`,l=Array(o+1).join(" "),c=[];for(let u=0;u<e;++u){let h=xT(u),d=u===0?"":l;c.push(d+r.replace(/{var}/g,h))}return c.join(a)}}});var C_=dn((pB,ma)=>{var M_=Vr();ma.exports=bT;ma.exports.generateCreateBodyFunctionBody=E_;ma.exports.getVectorCode=T_;ma.exports.getBodyCode=A_;function bT(i,e){let n=E_(i,e),{Body:r}=new Function(n)();return r}function E_(i,e){return`
${T_(i,e)}
${A_(i,e)}
return {Body: Body, Vector: Vector};
`}function A_(i){let e=M_(i),n=e("{var}",{join:", "});return`
function Body(${n}) {
  this.isPinned = false;
  this.pos = new Vector(${n});
  this.force = new Vector();
  this.velocity = new Vector();
  this.mass = 1;

  this.springCount = 0;
  this.springLength = 0;
}

Body.prototype.reset = function() {
  this.force.reset();
  this.springCount = 0;
  this.springLength = 0;
}

Body.prototype.setPosition = function (${n}) {
  ${e("this.pos.{var} = {var} || 0;",{indent:2})}
};`}function T_(i,e){let n=M_(i),r="";return e&&(r=`${n(`
   var v{var};
Object.defineProperty(this, '{var}', {
  set: function(v) { 
    if (!Number.isFinite(v)) throw new Error('Cannot set non-numbers to {var}');
    v{var} = v; 
  },
  get: function() { return v{var}; }
});`)}`),`function Vector(${n("{var}",{join:", "})}) {
  ${r}
    if (typeof arguments[0] === 'object') {
      // could be another vector
      let v = arguments[0];
      ${n('if (!Number.isFinite(v.{var})) throw new Error("Expected value is not a finite number at Vector constructor ({var})");',{indent:4})}
      ${n("this.{var} = v.{var};",{indent:4})}
    } else {
      ${n('this.{var} = typeof {var} === "number" ? {var} : 0;',{indent:4})}
    }
  }
  
  Vector.prototype.reset = function () {
    ${n("this.{var} = ",{join:""})}0;
  };`}});var N_=dn((mB,ur)=>{var Sd=Vr(),cr=bd();ur.exports=ST;ur.exports.generateQuadTreeFunctionBody=R_;ur.exports.getInsertStackCode=O_;ur.exports.getQuadNodeCode=D_;ur.exports.isSamePosition=P_;ur.exports.getChildBodyCode=L_;ur.exports.setChildBodyCode=I_;function ST(i){let e=R_(i);return new Function(e)()}function R_(i){let e=Sd(i),n=Math.pow(2,i);return`
${O_()}
${D_(i)}
${P_(i)}
${L_(i)}
${I_(i)}

function createQuadTree(options, random) {
  options = options || {};
  options.gravity = typeof options.gravity === 'number' ? options.gravity : -1;
  options.theta = typeof options.theta === 'number' ? options.theta : 0.8;

  var gravity = options.gravity;
  var updateQueue = [];
  var insertStack = new InsertStack();
  var theta = options.theta;

  var nodesCache = [];
  var currentInCache = 0;
  var root = newNode();

  return {
    insertBodies: insertBodies,

    /**
     * Gets root node if it is present
     */
    getRoot: function() {
      return root;
    },

    updateBodyForce: update,

    options: function(newOptions) {
      if (newOptions) {
        if (typeof newOptions.gravity === 'number') {
          gravity = newOptions.gravity;
        }
        if (typeof newOptions.theta === 'number') {
          theta = newOptions.theta;
        }

        return this;
      }

      return {
        gravity: gravity,
        theta: theta
      };
    }
  };

  function newNode() {
    // To avoid pressure on GC we reuse nodes.
    var node = nodesCache[currentInCache];
    if (node) {
${a("      node.")}
      node.body = null;
      node.mass = ${e("node.mass_{var} = ",{join:""})}0;
      ${e("node.min_{var} = node.max_{var} = ",{join:""})}0;
    } else {
      node = new QuadNode();
      nodesCache[currentInCache] = node;
    }

    ++currentInCache;
    return node;
  }

  function update(sourceBody) {
    var queue = updateQueue;
    var v;
    ${e("var d{var};",{indent:4})}
    var r; 
    ${e("var f{var} = 0;",{indent:4})}
    var queueLength = 1;
    var shiftIdx = 0;
    var pushIdx = 1;

    queue[0] = root;

    while (queueLength) {
      var node = queue[shiftIdx];
      var body = node.body;

      queueLength -= 1;
      shiftIdx += 1;
      var differentBody = (body !== sourceBody);
      if (body && differentBody) {
        // If the current node is a leaf node (and it is not source body),
        // calculate the force exerted by the current node on body, and add this
        // amount to body's net force.
        ${e("d{var} = body.pos.{var} - sourceBody.pos.{var};",{indent:8})}
        r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});

        if (r === 0) {
          // Poor man's protection against zero distance.
          ${e("d{var} = (random.nextDouble() - 0.5) / 50;",{indent:10})}
          r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});
        }

        // This is standard gravitation force calculation but we divide
        // by r^3 to save two operations when normalizing force vector.
        v = gravity * body.mass * sourceBody.mass / (r * r * r);
        ${e("f{var} += v * d{var};",{indent:8})}
      } else if (differentBody) {
        // Otherwise, calculate the ratio s / r,  where s is the width of the region
        // represented by the internal node, and r is the distance between the body
        // and the node's center-of-mass
        ${e("d{var} = node.mass_{var} / node.mass - sourceBody.pos.{var};",{indent:8})}
        r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});

        if (r === 0) {
          // Sorry about code duplication. I don't want to create many functions
          // right away. Just want to see performance first.
          ${e("d{var} = (random.nextDouble() - 0.5) / 50;",{indent:10})}
          r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});
        }
        // If s / r < \u03B8, treat this internal node as a single body, and calculate the
        // force it exerts on sourceBody, and add this amount to sourceBody's net force.
        if ((node.max_${cr(0)} - node.min_${cr(0)}) / r < theta) {
          // in the if statement above we consider node's width only
          // because the region was made into square during tree creation.
          // Thus there is no difference between using width or height.
          v = gravity * node.mass * sourceBody.mass / (r * r * r);
          ${e("f{var} += v * d{var};",{indent:10})}
        } else {
          // Otherwise, run the procedure recursively on each of the current node's children.

          // I intentionally unfolded this loop, to save several CPU cycles.
${o()}
        }
      }
    }

    ${e("sourceBody.force.{var} += f{var};",{indent:4})}
  }

  function insertBodies(bodies) {
    ${e("var {var}min = Number.MAX_VALUE;",{indent:4})}
    ${e("var {var}max = Number.MIN_VALUE;",{indent:4})}
    var i = bodies.length;

    // To reduce quad tree depth we are looking for exact bounding box of all particles.
    while (i--) {
      var pos = bodies[i].pos;
      ${e("if (pos.{var} < {var}min) {var}min = pos.{var};",{indent:6})}
      ${e("if (pos.{var} > {var}max) {var}max = pos.{var};",{indent:6})}
    }

    // Makes the bounds square.
    var maxSideLength = -Infinity;
    ${e("if ({var}max - {var}min > maxSideLength) maxSideLength = {var}max - {var}min ;",{indent:4})}

    currentInCache = 0;
    root = newNode();
    ${e("root.min_{var} = {var}min;",{indent:4})}
    ${e("root.max_{var} = {var}min + maxSideLength;",{indent:4})}

    i = bodies.length - 1;
    if (i >= 0) {
      root.body = bodies[i];
    }
    while (i--) {
      insert(bodies[i], root);
    }
  }

  function insert(newBody) {
    insertStack.reset();
    insertStack.push(root, newBody);

    while (!insertStack.isEmpty()) {
      var stackItem = insertStack.pop();
      var node = stackItem.node;
      var body = stackItem.body;

      if (!node.body) {
        // This is internal node. Update the total mass of the node and center-of-mass.
        ${e("var {var} = body.pos.{var};",{indent:8})}
        node.mass += body.mass;
        ${e("node.mass_{var} += body.mass * {var};",{indent:8})}

        // Recursively insert the body in the appropriate quadrant.
        // But first find the appropriate quadrant.
        var quadIdx = 0; // Assume we are in the 0's quad.
        ${e("var min_{var} = node.min_{var};",{indent:8})}
        ${e("var max_{var} = (min_{var} + node.max_{var}) / 2;",{indent:8})}

${s(8)}

        var child = getChild(node, quadIdx);

        if (!child) {
          // The node is internal but this quadrant is not taken. Add
          // subnode to it.
          child = newNode();
          ${e("child.min_{var} = min_{var};",{indent:10})}
          ${e("child.max_{var} = max_{var};",{indent:10})}
          child.body = body;

          setChild(node, quadIdx, child);
        } else {
          // continue searching in this quadrant.
          insertStack.push(child, body);
        }
      } else {
        // We are trying to add to the leaf node.
        // We have to convert current leaf into internal node
        // and continue adding two nodes.
        var oldBody = node.body;
        node.body = null; // internal nodes do not cary bodies

        if (isSamePosition(oldBody.pos, body.pos)) {
          // Prevent infinite subdivision by bumping one node
          // anywhere in this quadrant
          var retriesCount = 3;
          do {
            var offset = random.nextDouble();
            ${e("var d{var} = (node.max_{var} - node.min_{var}) * offset;",{indent:12})}

            ${e("oldBody.pos.{var} = node.min_{var} + d{var};",{indent:12})}
            retriesCount -= 1;
            // Make sure we don't bump it out of the box. If we do, next iteration should fix it
          } while (retriesCount > 0 && isSamePosition(oldBody.pos, body.pos));

          if (retriesCount === 0 && isSamePosition(oldBody.pos, body.pos)) {
            // This is very bad, we ran out of precision.
            // if we do not return from the method we'll get into
            // infinite loop here. So we sacrifice correctness of layout, and keep the app running
            // Next layout iteration should get larger bounding box in the first step and fix this
            return;
          }
        }
        // Next iteration should subdivide node further.
        insertStack.push(node, oldBody);
        insertStack.push(node, body);
      }
    }
  }
}
return createQuadTree;

`;function s(l){let c=[],u=Array(l+1).join(" ");for(let h=0;h<i;++h)c.push(u+`if (${cr(h)} > max_${cr(h)}) {`),c.push(u+`  quadIdx = quadIdx + ${Math.pow(2,h)};`),c.push(u+`  min_${cr(h)} = max_${cr(h)};`),c.push(u+`  max_${cr(h)} = node.max_${cr(h)};`),c.push(u+"}");return c.join(`
`)}function o(){let l=Array(11).join(" "),c=[];for(let u=0;u<n;++u)c.push(l+`if (node.quad${u}) {`),c.push(l+`  queue[pushIdx] = node.quad${u};`),c.push(l+"  queueLength += 1;"),c.push(l+"  pushIdx += 1;"),c.push(l+"}");return c.join(`
`)}function a(l){let c=[];for(let u=0;u<n;++u)c.push(`${l}quad${u} = null;`);return c.join(`
`)}}function P_(i){let e=Sd(i);return`
  function isSamePosition(point1, point2) {
    ${e("var d{var} = Math.abs(point1.{var} - point2.{var});",{indent:2})}
  
    return ${e("d{var} < 1e-8",{join:" && "})};
  }  
`}function I_(i){var e=Math.pow(2,i);return`
function setChild(node, idx, child) {
  ${n()}
}`;function n(){let r=[];for(let s=0;s<e;++s){let o=s===0?"  ":"  else ";r.push(`${o}if (idx === ${s}) node.quad${s} = child;`)}return r.join(`
`)}}function L_(i){return`function getChild(node, idx) {
${e()}
  return null;
}`;function e(){let n=[],r=Math.pow(2,i);for(let s=0;s<r;++s)n.push(`  if (idx === ${s}) return node.quad${s};`);return n.join(`
`)}}function D_(i){let e=Sd(i),n=Math.pow(2,i);var r=`
function QuadNode() {
  // body stored inside this node. In quad tree only leaf nodes (by construction)
  // contain bodies:
  this.body = null;

  // Child nodes are stored in quads. Each quad is presented by number:
  // 0 | 1
  // -----
  // 2 | 3
${s("  this.")}

  // Total mass of current node
  this.mass = 0;

  // Center of mass coordinates
  ${e("this.mass_{var} = 0;",{indent:2})}

  // bounding box coordinates
  ${e("this.min_{var} = 0;",{indent:2})}
  ${e("this.max_{var} = 0;",{indent:2})}
}
`;return r;function s(o){let a=[];for(let l=0;l<n;++l)a.push(`${o}quad${l} = null;`);return a.join(`
`)}}function O_(){return`
/**
 * Our implementation of QuadTree is non-recursive to avoid GC hit
 * This data structure represent stack of elements
 * which we are trying to insert into quad tree.
 */
function InsertStack () {
    this.stack = [];
    this.popIdx = 0;
}

InsertStack.prototype = {
    isEmpty: function() {
        return this.popIdx === 0;
    },
    push: function (node, body) {
        var item = this.stack[this.popIdx];
        if (!item) {
            // we are trying to avoid memory pressure: create new element
            // only when absolutely necessary
            this.stack[this.popIdx] = new InsertStackElement(node, body);
        } else {
            item.node = node;
            item.body = body;
        }
        ++this.popIdx;
    },
    pop: function () {
        if (this.popIdx > 0) {
            return this.stack[--this.popIdx];
        }
    },
    reset: function () {
        this.popIdx = 0;
    }
};

function InsertStackElement(node, body) {
    this.node = node; // QuadTree node
    this.body = body; // physical body which needs to be inserted to node
}
`}});var F_=dn((gB,wd)=>{wd.exports=MT;wd.exports.generateFunctionBody=U_;var wT=Vr();function MT(i){let e=U_(i);return new Function("bodies","settings","random",e)}function U_(i){let e=wT(i);return`
  var boundingBox = {
    ${e("min_{var}: 0, max_{var}: 0,",{indent:4})}
  };

  return {
    box: boundingBox,

    update: updateBoundingBox,

    reset: resetBoundingBox,

    getBestNewPosition: function (neighbors) {
      var ${e("base_{var} = 0",{join:", "})};

      if (neighbors.length) {
        for (var i = 0; i < neighbors.length; ++i) {
          let neighborPos = neighbors[i].pos;
          ${e("base_{var} += neighborPos.{var};",{indent:10})}
        }

        ${e("base_{var} /= neighbors.length;",{indent:8})}
      } else {
        ${e("base_{var} = (boundingBox.min_{var} + boundingBox.max_{var}) / 2;",{indent:8})}
      }

      var springLength = settings.springLength;
      return {
        ${e("{var}: base_{var} + (random.nextDouble() - 0.5) * springLength,",{indent:8})}
      };
    }
  };

  function updateBoundingBox() {
    var i = bodies.length;
    if (i === 0) return; // No bodies - no borders.

    ${e("var max_{var} = -Infinity;",{indent:4})}
    ${e("var min_{var} = Infinity;",{indent:4})}

    while(i--) {
      // this is O(n), it could be done faster with quadtree, if we check the root node bounds
      var bodyPos = bodies[i].pos;
      ${e("if (bodyPos.{var} < min_{var}) min_{var} = bodyPos.{var};",{indent:6})}
      ${e("if (bodyPos.{var} > max_{var}) max_{var} = bodyPos.{var};",{indent:6})}
    }

    ${e("boundingBox.min_{var} = min_{var};",{indent:4})}
    ${e("boundingBox.max_{var} = max_{var};",{indent:4})}
  }

  function resetBoundingBox() {
    ${e("boundingBox.min_{var} = boundingBox.max_{var} = 0;",{indent:4})}
  }
`}});var B_=dn((_B,Md)=>{var ET=Vr();Md.exports=AT;Md.exports.generateCreateDragForceFunctionBody=k_;function AT(i){let e=k_(i);return new Function("options",e)}function k_(i){return`
  if (!Number.isFinite(options.dragCoefficient)) throw new Error('dragCoefficient is not a finite number');

  return {
    update: function(body) {
      ${ET(i)("body.force.{var} -= options.dragCoefficient * body.velocity.{var};",{indent:6})}
    }
  };
`}});var V_=dn((vB,Ed)=>{var TT=Vr();Ed.exports=CT;Ed.exports.generateCreateSpringForceFunctionBody=z_;function CT(i){let e=z_(i);return new Function("options","random",e)}function z_(i){let e=TT(i);return`
  if (!Number.isFinite(options.springCoefficient)) throw new Error('Spring coefficient is not a number');
  if (!Number.isFinite(options.springLength)) throw new Error('Spring length is not a number');

  return {
    /**
     * Updates forces acting on a spring
     */
    update: function (spring) {
      var body1 = spring.from;
      var body2 = spring.to;
      var length = spring.length < 0 ? options.springLength : spring.length;
      ${e("var d{var} = body2.pos.{var} - body1.pos.{var};",{indent:6})}
      var r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});

      if (r === 0) {
        ${e("d{var} = (random.nextDouble() - 0.5) / 50;",{indent:8})}
        r = Math.sqrt(${e("d{var} * d{var}",{join:" + "})});
      }

      var d = r - length;
      var coefficient = ((spring.coefficient > 0) ? spring.coefficient : options.springCoefficient) * d / r;

      ${e("body1.force.{var} += coefficient * d{var}",{indent:6})};
      body1.springCount += 1;
      body1.springLength += r;

      ${e("body2.force.{var} -= coefficient * d{var}",{indent:6})};
      body2.springCount += 1;
      body2.springLength += r;
    }
  };
`}});var H_=dn((yB,Ad)=>{var RT=Vr();Ad.exports=PT;Ad.exports.generateIntegratorFunctionBody=G_;function PT(i){let e=G_(i);return new Function("bodies","timeStep","adaptiveTimeStepWeight",e)}function G_(i){let e=RT(i);return`
  var length = bodies.length;
  if (length === 0) return 0;

  ${e("var d{var} = 0, t{var} = 0;",{indent:2})}

  for (var i = 0; i < length; ++i) {
    var body = bodies[i];
    if (body.isPinned) continue;

    if (adaptiveTimeStepWeight && body.springCount) {
      timeStep = (adaptiveTimeStepWeight * body.springLength/body.springCount);
    }

    var coeff = timeStep / body.mass;

    ${e("body.velocity.{var} += coeff * body.force.{var};",{indent:4})}
    ${e("var v{var} = body.velocity.{var};",{indent:4})}
    var v = Math.sqrt(${e("v{var} * v{var}",{join:" + "})});

    if (v > 1) {
      // We normalize it so that we move within timeStep range. 
      // for the case when v <= 1 - we let velocity to fade out.
      ${e("body.velocity.{var} = v{var} / v;",{indent:6})}
    }

    ${e("d{var} = timeStep * body.velocity.{var};",{indent:4})}

    ${e("body.pos.{var} += d{var};",{indent:4})}

    ${e("t{var} += Math.abs(d{var});",{indent:4})}
  }

  return (${e("t{var} * t{var}",{join:" + "})})/length;
`}});var X_=dn((xB,W_)=>{W_.exports=IT;function IT(i,e,n,r){this.from=i,this.to=e,this.length=n,this.coefficient=r}});var Y_=dn((bB,j_)=>{j_.exports=q_;function q_(i,e){var n;if(i||(i={}),e){for(n in e)if(e.hasOwnProperty(n)){var r=i.hasOwnProperty(n),s=typeof e[n],o=!r||typeof i[n]!==s;o?i[n]=e[n]:s==="object"&&(i[n]=q_(i[n],e[n]))}}return i}});var Td=dn((SB,$_)=>{"use strict";function LT(i){OT(i);let e=DT(i);return i.on=e.on,i.off=e.off,i.fire=e.fire,i}function DT(i){let e=Object.create(null);return{on:function(n,r,s){if(typeof r!="function")throw new Error("callback is expected to be a function");let o=e[n];return o||(o=e[n]=[]),o.push({callback:r,ctx:s}),i},off:function(n,r){if(typeof n>"u")return e=Object.create(null),i;if(e[n])if(typeof r!="function")delete e[n];else{let s=e[n];for(let o=0;o<s.length;++o)s[o].callback===r&&s.splice(o,1)}return i},fire:function(n){let r=e[n];if(!r)return i;let s;arguments.length>1&&(s=Array.prototype.slice.call(arguments,1));for(let o=0;o<r.length;++o){let a=r[o];a.callback.apply(a.ctx,s)}return i}}}function OT(i){if(!i)throw new Error("Eventify cannot use falsy object as events subject");let e=["on","fire","off"];for(let n=0;n<e.length;++n)if(i.hasOwnProperty(e[n]))throw new Error("Subject cannot be eventified, since it already has property '"+e[n]+"'")}$_.exports=LT});var K_=dn((wB,vu)=>{vu.exports=Cd;vu.exports.random=Cd,vu.exports.randomIterator=kT;function Cd(i){var e=typeof i=="number"?i:+new Date;return new Gr(e)}function Gr(i){this.seed=i}Gr.prototype.next=FT;Gr.prototype.nextDouble=Rd;Gr.prototype.uniform=Rd;Gr.prototype.gaussian=NT;Gr.prototype.random=Rd;function NT(){var i,e,n;do e=this.nextDouble()*2-1,n=this.nextDouble()*2-1,i=e*e+n*n;while(i>=1||i===0);return e*Math.sqrt(-2*Math.log(i)/i)}Gr.prototype.levy=UT;function UT(){var i=1.5,e=Math.pow(Z_(1+i)*Math.sin(Math.PI*i/2)/(Z_((1+i)/2)*i*Math.pow(2,(i-1)/2)),1/i);return this.gaussian()*e/Math.pow(Math.abs(this.gaussian()),1/i)}function Z_(i){return Math.sqrt(2*Math.PI/i)*Math.pow(1/Math.E*(i+1/(12*i-1/(10*i))),i)}function Rd(){var i=this.seed;return i=i+2127912214+(i<<12)&4294967295,i=(i^3345072700^i>>>19)&4294967295,i=i+374761393+(i<<5)&4294967295,i=(i+3550635116^i<<9)&4294967295,i=i+4251993797+(i<<3)&4294967295,i=(i^3042594569^i>>>16)&4294967295,this.seed=i,(i&268435455)/268435456}function FT(i){return Math.floor(this.nextDouble()*i)}function kT(i,e){var n=e||Cd();if(typeof n.next!="function")throw new Error("customRandom does not match expected API: next() function is missing");return{forEach:s,shuffle:r};function r(){var o,a,l;for(o=i.length-1;o>0;--o)a=n.next(o+1),l=i[a],i[a]=i[o],i[o]=l;return i}function s(o){var a,l,c;for(a=i.length-1;a>0;--a)l=n.next(a+1),c=i[l],i[l]=i[a],i[a]=c,o(c);i.length&&o(i[0])}}});var Pd=dn((MB,Q_)=>{Q_.exports=XT;var BT=C_(),zT=N_(),VT=F_(),GT=B_(),HT=V_(),WT=H_(),J_={};function XT(i){var e=X_(),n=Y_(),r=Td();if(i){if(i.springCoeff!==void 0)throw new Error("springCoeff was renamed to springCoefficient");if(i.dragCoeff!==void 0)throw new Error("dragCoeff was renamed to dragCoefficient")}i=n(i,{springLength:10,springCoefficient:.8,gravity:-12,theta:.8,dragCoefficient:.9,timeStep:.5,adaptiveTimeStepWeight:0,dimensions:2,debug:!1});var s=J_[i.dimensions];if(!s){var o=i.dimensions;s={Body:BT(o,i.debug),createQuadTree:zT(o),createBounds:VT(o),createDragForce:GT(o),createSpringForce:HT(o),integrate:WT(o)},J_[o]=s}var a=s.Body,l=s.createQuadTree,c=s.createBounds,u=s.createDragForce,h=s.createSpringForce,d=s.integrate,f=I=>new a(I),p=K_().random(42),g=[],v=[],_=l(i,p),m=c(g,i,p),w=h(i,p),T=u(i),y=0,b=[],S=new Map,A=0;L("nbody",U),L("spring",M);var x={bodies:g,quadTree:_,springs:v,settings:i,addForce:L,removeForce:P,getForces:O,step:function(){for(var I=0;I<b.length;++I)b[I](A);var D=d(g,i.timeStep,i.adaptiveTimeStepWeight);return A+=1,D},addBody:function(I){if(!I)throw new Error("Body is required");return g.push(I),I},addBodyAt:function(I){if(!I)throw new Error("Body position is required");var D=f(I);return g.push(D),D},removeBody:function(I){if(I){var D=g.indexOf(I);if(!(D<0))return g.splice(D,1),g.length===0&&m.reset(),!0}},addSpring:function(I,D,k,q){if(!I||!D)throw new Error("Cannot add null spring to force simulator");typeof k!="number"&&(k=-1);var W=new e(I,D,k,q>=0?q:-1);return v.push(W),W},getTotalMovement:function(){return y},removeSpring:function(I){if(I){var D=v.indexOf(I);if(D>-1)return v.splice(D,1),!0}},getBestNewBodyPosition:function(I){return m.getBestNewPosition(I)},getBBox:C,getBoundingBox:C,invalidateBBox:function(){console.warn("invalidateBBox() is deprecated, bounds always recomputed on `getBBox()` call")},gravity:function(I){return I!==void 0?(i.gravity=I,_.options({gravity:I}),this):i.gravity},theta:function(I){return I!==void 0?(i.theta=I,_.options({theta:I}),this):i.theta},random:p};return qT(i,x),r(x),x;function C(){return m.update(),m.box}function L(I,D){if(S.has(I))throw new Error("Force "+I+" is already added");S.set(I,D),b.push(D)}function P(I){var D=b.indexOf(S.get(I));D<0||(b.splice(D,1),S.delete(I))}function O(){return S}function U(){if(g.length!==0){_.insertBodies(g);for(var I=g.length;I--;){var D=g[I];D.isPinned||(D.reset(),_.updateBodyForce(D),T.update(D))}}}function M(){for(var I=v.length;I--;)w.update(v[I])}}function qT(i,e){for(var n in i)jT(i,e,n)}function jT(i,e,n){if(i.hasOwnProperty(n)&&typeof e[n]!="function"){var r=Number.isFinite(i[n]);r?e[n]=function(s){if(s!==void 0){if(!Number.isFinite(s))throw new Error("Value of "+n+" should be a valid number.");return i[n]=s,e}return i[n]}:e[n]=function(s){return s!==void 0?(i[n]=s,e):i[n]}}}});var ev=dn((EB,Id)=>{Id.exports=$T;Id.exports.simulator=Pd();var YT=Td();function $T(i,e){if(!i)throw new Error("Graph structure cannot be undefined");var n=e&&e.createSimulator||Pd(),r=n(e);if(Array.isArray(e))throw new Error("Physics settings is expected to be an object");var s=i.version>19?U:O;e&&typeof e.nodeMass=="function"&&(s=e.nodeMass);var o=new Map,a={},l=0,c=r.settings.springTransform||ZT;T(),_();var u=!1,h={step:function(){if(l===0)return d(!0),!0;var M=r.step();h.lastMove=M,h.fire("step");var I=M/l,D=I<=.01;return d(D),D},getNodePosition:function(M){return P(M).pos},setNodePosition:function(M){var I=P(M);I.setPosition.apply(I,Array.prototype.slice.call(arguments,1))},getLinkPosition:function(M){var I=a[M];if(I)return{from:I.from.pos,to:I.to.pos}},getGraphRect:function(){return r.getBBox()},forEachBody:f,pinNode:function(M,I){var D=P(M.id);D.isPinned=!!I},isNodePinned:function(M){return P(M.id).isPinned},dispose:function(){i.off("changed",w),h.fire("disposed")},getBody:v,getSpring:g,getForceVectorLength:p,simulator:r,graph:i,lastMove:0};return YT(h),h;function d(M){u!==M&&(u=M,m(M))}function f(M){o.forEach(M)}function p(){var M=0,I=0;return f(function(D){M+=Math.abs(D.force.x),I+=Math.abs(D.force.y)}),Math.sqrt(M*M+I*I)}function g(M,I){var D;if(I===void 0)typeof M!="object"?D=M:D=M.id;else{var k=i.hasLink(M,I);if(!k)return;D=k.id}return a[D]}function v(M){return o.get(M)}function _(){i.on("changed",w)}function m(M){h.fire("stable",M)}function w(M){for(var I=0;I<M.length;++I){var D=M[I];D.changeType==="add"?(D.node&&y(D.node.id),D.link&&S(D.link)):D.changeType==="remove"&&(D.node&&b(D.node),D.link&&A(D.link))}l=i.getNodesCount()}function T(){l=0,i.forEachNode(function(M){y(M.id),l+=1}),i.forEachLink(S)}function y(M){var I=o.get(M);if(!I){var D=i.getNode(M);if(!D)throw new Error("initBody() was called with unknown node id");var k=D.position;if(!k){var q=x(D);k=r.getBestNewBodyPosition(q)}I=r.addBodyAt(k),I.id=M,o.set(M,I),C(M),L(D)&&(I.isPinned=!0)}}function b(M){var I=M.id,D=o.get(I);D&&(o.delete(I),r.removeBody(D))}function S(M){C(M.fromId),C(M.toId);var I=o.get(M.fromId),D=o.get(M.toId),k=r.addSpring(I,D,M.length);c(M,k),a[M.id]=k}function A(M){var I=a[M.id];if(I){var D=i.getNode(M.fromId),k=i.getNode(M.toId);D&&C(D.id),k&&C(k.id),delete a[M.id],r.removeSpring(I)}}function x(M){var I=[];if(!M.links)return I;for(var D=Math.min(M.links.length,2),k=0;k<D;++k){var q=M.links[k],W=q.fromId!==M.id?o.get(q.fromId):o.get(q.toId);W&&W.pos&&I.push(W)}return I}function C(M){var I=o.get(M);if(I.mass=s(M),Number.isNaN(I.mass))throw new Error("Node mass should be a number")}function L(M){return M&&(M.isPinned||M.data&&M.data.isPinned)}function P(M){var I=o.get(M);return I||(y(M),I=o.get(M)),I}function O(M){var I=i.getLinks(M);return I?1+I.length/3:1}function U(M){var I=i.getLinks(M);return I?1+I.size/3:1}}function ZT(){}});var Et={LEFT:0,MIDDLE:1,RIGHT:2,ROTATE:0,DOLLY:1,PAN:2},Dn={ROTATE:0,PAN:1,DOLLY_PAN:2,DOLLY_ROTATE:3},Gm=0,_f=1,Hm=2;var Ho=1,Wm=2,Fs=3,ir=0,Gt=1,di=2,On=0,ks=1,vf=2,yf=3,xf=4,Xm=5;var Ur=100,qm=101,jm=102,Ym=103,$m=104,Zm=200,Km=201,Jm=202,Qm=203,bf=204,Sf=205,eg=206,tg=207,ng=208,ig=209,rg=210,sg=211,og=212,ag=213,lg=214,Ml=0,El=1,Al=2,Ss=3,Tl=4,Cl=5,Rl=6,Pl=7,hc=0,cg=1,ug=2,Zn=0,wf=1,Mf=2,Ef=3,Af=4,Tf=5,Cf=6,Rf=7;var Pf=300,rr=301,Fr=302,fc=303,dc=304,Wo=306,Il=1e3,ui=1001,Ll=1002,Nt=1003,hg=1004;var Xo=1005;var Vt=1006,pc=1007;var sr=1008;var mn=1009,If=1010,Lf=1011,Bs=1012,mc=1013,Kn=1014,Jn=1015,En=1016,gc=1017,_c=1018,zs=1020,Df=35902,Of=35899,Nf=1021,Uf=1022,Nn=1023,fi=1026,or=1027,Ff=1028,vc=1029,ar=1030,yc=1031;var xc=1033,qo=33776,jo=33777,Yo=33778,$o=33779,bc=35840,Sc=35841,wc=35842,Mc=35843,Ec=36196,Ac=37492,Tc=37496,Cc=37488,Rc=37489,Zo=37490,Pc=37491,Ic=37808,Lc=37809,Dc=37810,Oc=37811,Nc=37812,Uc=37813,Fc=37814,kc=37815,Bc=37816,zc=37817,Vc=37818,Gc=37819,Hc=37820,Wc=37821,Xc=36492,qc=36494,jc=36495,Yc=36283,$c=36284,Ko=36285,Zc=36286;var So=2300,Dl=2301,bl=2302,cf=2303,uf=2400,hf=2401,ff=2402;var fg=3200;var Kc=0,dg=1,Pi="",on="srgb",wo="srgb-linear",Mo="linear",rt="srgb";var Sl=7680;var pg=519,mg=512,gg=513,_g=514,Jc=515,vg=516,yg=517,Qc=518,xg=519,bg=35044;var kf="300 es",jn=2e3,ws=2001;function aS(i){for(let e=i.length-1;e>=0;--e)if(i[e]>=65535)return!0;return!1}function lS(i){return ArrayBuffer.isView(i)&&!(i instanceof DataView)}function Ms(i){return document.createElementNS("http://www.w3.org/1999/xhtml",i)}function Sg(){let i=Ms("canvas");return i.style.display="block",i}var fm={},Es=null;function Bf(...i){let e="THREE."+i.shift();Es?Es("log",e,...i):console.log(e,...i)}function wg(i){let e=i[0];if(typeof e=="string"&&e.startsWith("TSL:")){let n=i[1];n&&n.isStackTrace?i[0]+=" "+n.getLocation():i[1]='Stack trace not available. Enable "THREE.Node.captureStackTrace" to capture stack traces.'}return i}function Ue(...i){i=wg(i);let e="THREE."+i.shift();if(Es)Es("warn",e,...i);else{let n=i[0];n&&n.isStackTrace?console.warn(n.getError(e)):console.warn(e,...i)}}function ze(...i){i=wg(i);let e="THREE."+i.shift();if(Es)Es("error",e,...i);else{let n=i[0];n&&n.isStackTrace?console.error(n.getError(e)):console.error(e,...i)}}function Rr(...i){let e=i.join(" ");e in fm||(fm[e]=!0,Ue(...i))}function Mg(i,e,n){return new Promise(function(r,s){function o(){switch(i.clientWaitSync(e,i.SYNC_FLUSH_COMMANDS_BIT,0)){case i.WAIT_FAILED:s();break;case i.TIMEOUT_EXPIRED:setTimeout(o,n);break;default:r()}}setTimeout(o,n)})}var Eg={[Ml]:El,[Al]:Rl,[Tl]:Pl,[Ss]:Cl,[El]:Ml,[Rl]:Al,[Pl]:Tl,[Cl]:Ss},Yn=class{addEventListener(e,n){this._listeners===void 0&&(this._listeners={});let r=this._listeners;r[e]===void 0&&(r[e]=[]),r[e].indexOf(n)===-1&&r[e].push(n)}hasEventListener(e,n){let r=this._listeners;return r===void 0?!1:r[e]!==void 0&&r[e].indexOf(n)!==-1}removeEventListener(e,n){let r=this._listeners;if(r===void 0)return;let s=r[e];if(s!==void 0){let o=s.indexOf(n);o!==-1&&s.splice(o,1)}}dispatchEvent(e){let n=this._listeners;if(n===void 0)return;let r=n[e.type];if(r!==void 0){e.target=this;let s=r.slice(0);for(let o=0,a=s.length;o<a;o++)s[o].call(this,e);e.target=null}}},jt=["00","01","02","03","04","05","06","07","08","09","0a","0b","0c","0d","0e","0f","10","11","12","13","14","15","16","17","18","19","1a","1b","1c","1d","1e","1f","20","21","22","23","24","25","26","27","28","29","2a","2b","2c","2d","2e","2f","30","31","32","33","34","35","36","37","38","39","3a","3b","3c","3d","3e","3f","40","41","42","43","44","45","46","47","48","49","4a","4b","4c","4d","4e","4f","50","51","52","53","54","55","56","57","58","59","5a","5b","5c","5d","5e","5f","60","61","62","63","64","65","66","67","68","69","6a","6b","6c","6d","6e","6f","70","71","72","73","74","75","76","77","78","79","7a","7b","7c","7d","7e","7f","80","81","82","83","84","85","86","87","88","89","8a","8b","8c","8d","8e","8f","90","91","92","93","94","95","96","97","98","99","9a","9b","9c","9d","9e","9f","a0","a1","a2","a3","a4","a5","a6","a7","a8","a9","aa","ab","ac","ad","ae","af","b0","b1","b2","b3","b4","b5","b6","b7","b8","b9","ba","bb","bc","bd","be","bf","c0","c1","c2","c3","c4","c5","c6","c7","c8","c9","ca","cb","cc","cd","ce","cf","d0","d1","d2","d3","d4","d5","d6","d7","d8","d9","da","db","dc","dd","de","df","e0","e1","e2","e3","e4","e5","e6","e7","e8","e9","ea","eb","ec","ed","ee","ef","f0","f1","f2","f3","f4","f5","f6","f7","f8","f9","fa","fb","fc","fd","fe","ff"],dm=1234567,vo=Math.PI/180,As=180/Math.PI;function Vs(){let i=Math.random()*4294967295|0,e=Math.random()*4294967295|0,n=Math.random()*4294967295|0,r=Math.random()*4294967295|0;return(jt[i&255]+jt[i>>8&255]+jt[i>>16&255]+jt[i>>24&255]+"-"+jt[e&255]+jt[e>>8&255]+"-"+jt[e>>16&15|64]+jt[e>>24&255]+"-"+jt[n&63|128]+jt[n>>8&255]+"-"+jt[n>>16&255]+jt[n>>24&255]+jt[r&255]+jt[r>>8&255]+jt[r>>16&255]+jt[r>>24&255]).toLowerCase()}function je(i,e,n){return Math.max(e,Math.min(n,i))}function zf(i,e){return(i%e+e)%e}function cS(i,e,n,r,s){return r+(i-e)*(s-r)/(n-e)}function uS(i,e,n){return i!==e?(n-i)/(e-i):0}function yo(i,e,n){return(1-n)*i+n*e}function hS(i,e,n,r){return yo(i,e,1-Math.exp(-n*r))}function fS(i,e=1){return e-Math.abs(zf(i,e*2)-e)}function dS(i,e,n){return i<=e?0:i>=n?1:(i=(i-e)/(n-e),i*i*(3-2*i))}function pS(i,e,n){return i<=e?0:i>=n?1:(i=(i-e)/(n-e),i*i*i*(i*(i*6-15)+10))}function mS(i,e){return i+Math.floor(Math.random()*(e-i+1))}function gS(i,e){return i+Math.random()*(e-i)}function _S(i){return i*(.5-Math.random())}function vS(i){i!==void 0&&(dm=i);let e=dm+=1831565813;return e=Math.imul(e^e>>>15,e|1),e^=e+Math.imul(e^e>>>7,e|61),((e^e>>>14)>>>0)/4294967296}function yS(i){return i*vo}function xS(i){return i*As}function bS(i){return i>0&&Number.isInteger(i)&&2**Math.round(Math.log2(i))===i}function SS(i){return Math.pow(2,Math.ceil(Math.log(i)/Math.LN2))}function wS(i){return Math.pow(2,Math.floor(Math.log(i)/Math.LN2))}function MS(i,e,n,r,s){let o=Math.cos,a=Math.sin,l=o(n/2),c=a(n/2),u=o((e+r)/2),h=a((e+r)/2),d=o((e-r)/2),f=a((e-r)/2),p=o((r-e)/2),g=a((r-e)/2);switch(s){case"XYX":i.set(l*h,c*d,c*f,l*u);break;case"YZY":i.set(c*f,l*h,c*d,l*u);break;case"ZXZ":i.set(c*d,c*f,l*h,l*u);break;case"XZX":i.set(l*h,c*g,c*p,l*u);break;case"YXY":i.set(c*p,l*h,c*g,l*u);break;case"ZYZ":i.set(c*g,c*p,l*h,l*u);break;default:Ue("MathUtils: .setQuaternionFromProperEuler() encountered an unknown order: "+s)}}function xs(i,e){switch(e.constructor){case Float32Array:return i;case Uint32Array:return i/4294967295;case Uint16Array:return i/65535;case Uint8Array:case Uint8ClampedArray:return i/255;case Int32Array:return Math.max(i/2147483647,-1);case Int16Array:return Math.max(i/32767,-1);case Int8Array:return Math.max(i/127,-1);default:throw new Error("THREE.MathUtils: Invalid component type.")}}function sn(i,e){switch(e.constructor){case Float32Array:return i;case Uint32Array:return Math.round(i*4294967295);case Uint16Array:return Math.round(i*65535);case Uint8Array:case Uint8ClampedArray:return Math.round(i*255);case Int32Array:return Math.round(i*2147483647);case Int16Array:return Math.round(i*32767);case Int8Array:return Math.round(i*127);default:throw new Error("THREE.MathUtils: Invalid component type.")}}var Gs={DEG2RAD:vo,RAD2DEG:As,generateUUID:Vs,clamp:je,euclideanModulo:zf,mapLinear:cS,inverseLerp:uS,lerp:yo,damp:hS,pingpong:fS,smoothstep:dS,smootherstep:pS,randInt:mS,randFloat:gS,randFloatSpread:_S,seededRandom:vS,degToRad:yS,radToDeg:xS,isPowerOfTwo:bS,ceilPowerOfTwo:SS,floorPowerOfTwo:wS,setQuaternionFromProperEuler:MS,normalize:sn,denormalize:xs},qf=class qf{constructor(e=0,n=0){this.x=e,this.y=n}get width(){return this.x}set width(e){this.x=e}get height(){return this.y}set height(e){this.y=e}set(e,n){return this.x=e,this.y=n,this}setScalar(e){return this.x=e,this.y=e,this}setX(e){return this.x=e,this}setY(e){return this.y=e,this}setComponent(e,n){switch(e){case 0:this.x=n;break;case 1:this.y=n;break;default:throw new Error("THREE.Vector2: index is out of range: "+e)}return this}getComponent(e){switch(e){case 0:return this.x;case 1:return this.y;default:throw new Error("THREE.Vector2: index is out of range: "+e)}}clone(){return new this.constructor(this.x,this.y)}copy(e){return this.x=e.x,this.y=e.y,this}add(e){return this.x+=e.x,this.y+=e.y,this}addScalar(e){return this.x+=e,this.y+=e,this}addVectors(e,n){return this.x=e.x+n.x,this.y=e.y+n.y,this}addScaledVector(e,n){return this.x+=e.x*n,this.y+=e.y*n,this}sub(e){return this.x-=e.x,this.y-=e.y,this}subScalar(e){return this.x-=e,this.y-=e,this}subVectors(e,n){return this.x=e.x-n.x,this.y=e.y-n.y,this}multiply(e){return this.x*=e.x,this.y*=e.y,this}multiplyScalar(e){return this.x*=e,this.y*=e,this}divide(e){return this.x/=e.x,this.y/=e.y,this}divideScalar(e){return this.multiplyScalar(1/e)}applyMatrix3(e){let n=this.x,r=this.y,s=e.elements;return this.x=s[0]*n+s[3]*r+s[6],this.y=s[1]*n+s[4]*r+s[7],this}min(e){return this.x=Math.min(this.x,e.x),this.y=Math.min(this.y,e.y),this}max(e){return this.x=Math.max(this.x,e.x),this.y=Math.max(this.y,e.y),this}clamp(e,n){return this.x=je(this.x,e.x,n.x),this.y=je(this.y,e.y,n.y),this}clampScalar(e,n){return this.x=je(this.x,e,n),this.y=je(this.y,e,n),this}clampLength(e,n){let r=this.length();return this.divideScalar(r||1).multiplyScalar(je(r,e,n))}floor(){return this.x=Math.floor(this.x),this.y=Math.floor(this.y),this}ceil(){return this.x=Math.ceil(this.x),this.y=Math.ceil(this.y),this}round(){return this.x=Math.round(this.x),this.y=Math.round(this.y),this}roundToZero(){return this.x=Math.trunc(this.x),this.y=Math.trunc(this.y),this}negate(){return this.x=-this.x,this.y=-this.y,this}dot(e){return this.x*e.x+this.y*e.y}cross(e){return this.x*e.y-this.y*e.x}lengthSq(){return this.x*this.x+this.y*this.y}length(){return Math.sqrt(this.x*this.x+this.y*this.y)}manhattanLength(){return Math.abs(this.x)+Math.abs(this.y)}normalize(){return this.divideScalar(this.length()||1)}angle(){return Math.atan2(-this.y,-this.x)+Math.PI}angleTo(e){let n=Math.sqrt(this.lengthSq()*e.lengthSq());if(n===0)return Math.PI/2;let r=this.dot(e)/n;return Math.acos(je(r,-1,1))}distanceTo(e){return Math.sqrt(this.distanceToSquared(e))}distanceToSquared(e){let n=this.x-e.x,r=this.y-e.y;return n*n+r*r}manhattanDistanceTo(e){return Math.abs(this.x-e.x)+Math.abs(this.y-e.y)}setLength(e){return this.normalize().multiplyScalar(e)}lerp(e,n){return this.x+=(e.x-this.x)*n,this.y+=(e.y-this.y)*n,this}lerpVectors(e,n,r){return this.x=e.x+(n.x-e.x)*r,this.y=e.y+(n.y-e.y)*r,this}equals(e){return e.x===this.x&&e.y===this.y}fromArray(e,n=0){return this.x=e[n],this.y=e[n+1],this}toArray(e=[],n=0){return e[n]=this.x,e[n+1]=this.y,e}fromBufferAttribute(e,n){return this.x=e.getX(n),this.y=e.getY(n),this}rotateAround(e,n){let r=Math.cos(n),s=Math.sin(n),o=this.x-e.x,a=this.y-e.y;return this.x=o*r-a*s+e.x,this.y=o*s+a*r+e.y,this}random(){return this.x=Math.random(),this.y=Math.random(),this}*[Symbol.iterator](){yield this.x,yield this.y}};qf.prototype.isVector2=!0;var fe=qf,Ut=class{constructor(e=0,n=0,r=0,s=1){this.isQuaternion=!0,this._x=e,this._y=n,this._z=r,this._w=s}static slerpFlat(e,n,r,s,o,a,l){let c=r[s+0],u=r[s+1],h=r[s+2],d=r[s+3],f=o[a+0],p=o[a+1],g=o[a+2],v=o[a+3];if(d!==v||c!==f||u!==p||h!==g){let _=c*f+u*p+h*g+d*v;_<0&&(f=-f,p=-p,g=-g,v=-v,_=-_);let m=1-l;if(_<.9995){let w=Math.acos(_),T=Math.sin(w);m=Math.sin(m*w)/T,l=Math.sin(l*w)/T,c=c*m+f*l,u=u*m+p*l,h=h*m+g*l,d=d*m+v*l}else{c=c*m+f*l,u=u*m+p*l,h=h*m+g*l,d=d*m+v*l;let w=1/Math.sqrt(c*c+u*u+h*h+d*d);c*=w,u*=w,h*=w,d*=w}}e[n]=c,e[n+1]=u,e[n+2]=h,e[n+3]=d}static multiplyQuaternionsFlat(e,n,r,s,o,a){let l=r[s],c=r[s+1],u=r[s+2],h=r[s+3],d=o[a],f=o[a+1],p=o[a+2],g=o[a+3];return e[n]=l*g+h*d+c*p-u*f,e[n+1]=c*g+h*f+u*d-l*p,e[n+2]=u*g+h*p+l*f-c*d,e[n+3]=h*g-l*d-c*f-u*p,e}get x(){return this._x}set x(e){this._x=e,this._onChangeCallback()}get y(){return this._y}set y(e){this._y=e,this._onChangeCallback()}get z(){return this._z}set z(e){this._z=e,this._onChangeCallback()}get w(){return this._w}set w(e){this._w=e,this._onChangeCallback()}set(e,n,r,s){return this._x=e,this._y=n,this._z=r,this._w=s,this._onChangeCallback(),this}clone(){return new this.constructor(this._x,this._y,this._z,this._w)}copy(e){return this._x=e.x,this._y=e.y,this._z=e.z,this._w=e.w,this._onChangeCallback(),this}setFromEuler(e,n=!0){let r=e._x,s=e._y,o=e._z,a=e._order,l=Math.cos,c=Math.sin,u=l(r/2),h=l(s/2),d=l(o/2),f=c(r/2),p=c(s/2),g=c(o/2);switch(a){case"XYZ":this._x=f*h*d+u*p*g,this._y=u*p*d-f*h*g,this._z=u*h*g+f*p*d,this._w=u*h*d-f*p*g;break;case"YXZ":this._x=f*h*d+u*p*g,this._y=u*p*d-f*h*g,this._z=u*h*g-f*p*d,this._w=u*h*d+f*p*g;break;case"ZXY":this._x=f*h*d-u*p*g,this._y=u*p*d+f*h*g,this._z=u*h*g+f*p*d,this._w=u*h*d-f*p*g;break;case"ZYX":this._x=f*h*d-u*p*g,this._y=u*p*d+f*h*g,this._z=u*h*g-f*p*d,this._w=u*h*d+f*p*g;break;case"YZX":this._x=f*h*d+u*p*g,this._y=u*p*d+f*h*g,this._z=u*h*g-f*p*d,this._w=u*h*d-f*p*g;break;case"XZY":this._x=f*h*d-u*p*g,this._y=u*p*d-f*h*g,this._z=u*h*g+f*p*d,this._w=u*h*d+f*p*g;break;default:Ue("Quaternion: .setFromEuler() encountered an unknown order: "+a)}return n===!0&&this._onChangeCallback(),this}setFromAxisAngle(e,n){let r=n/2,s=Math.sin(r);return this._x=e.x*s,this._y=e.y*s,this._z=e.z*s,this._w=Math.cos(r),this._onChangeCallback(),this}setFromRotationMatrix(e){let n=e.elements,r=n[0],s=n[4],o=n[8],a=n[1],l=n[5],c=n[9],u=n[2],h=n[6],d=n[10],f=r+l+d;if(f>0){let p=.5/Math.sqrt(f+1);this._w=.25/p,this._x=(h-c)*p,this._y=(o-u)*p,this._z=(a-s)*p}else if(r>l&&r>d){let p=2*Math.sqrt(1+r-l-d);this._w=(h-c)/p,this._x=.25*p,this._y=(s+a)/p,this._z=(o+u)/p}else if(l>d){let p=2*Math.sqrt(1+l-r-d);this._w=(o-u)/p,this._x=(s+a)/p,this._y=.25*p,this._z=(c+h)/p}else{let p=2*Math.sqrt(1+d-r-l);this._w=(a-s)/p,this._x=(o+u)/p,this._y=(c+h)/p,this._z=.25*p}return this._onChangeCallback(),this}setFromUnitVectors(e,n){let r=e.dot(n)+1;return r<1e-8?(r=0,Math.abs(e.x)>Math.abs(e.z)?(this._x=-e.y,this._y=e.x,this._z=0,this._w=r):(this._x=0,this._y=-e.z,this._z=e.y,this._w=r)):(this._x=e.y*n.z-e.z*n.y,this._y=e.z*n.x-e.x*n.z,this._z=e.x*n.y-e.y*n.x,this._w=r),this.normalize()}angleTo(e){return 2*Math.acos(Math.abs(je(this.dot(e),-1,1)))}rotateTowards(e,n){let r=this.angleTo(e);if(r===0)return this;let s=Math.min(1,n/r);return this.slerp(e,s),this}identity(){return this.set(0,0,0,1)}invert(){return this.conjugate()}conjugate(){return this._x*=-1,this._y*=-1,this._z*=-1,this._onChangeCallback(),this}dot(e){return this._x*e._x+this._y*e._y+this._z*e._z+this._w*e._w}lengthSq(){return this._x*this._x+this._y*this._y+this._z*this._z+this._w*this._w}length(){return Math.sqrt(this._x*this._x+this._y*this._y+this._z*this._z+this._w*this._w)}normalize(){let e=this.length();return e===0?(this._x=0,this._y=0,this._z=0,this._w=1):(e=1/e,this._x=this._x*e,this._y=this._y*e,this._z=this._z*e,this._w=this._w*e),this._onChangeCallback(),this}multiply(e){return this.multiplyQuaternions(this,e)}premultiply(e){return this.multiplyQuaternions(e,this)}multiplyQuaternions(e,n){let r=e._x,s=e._y,o=e._z,a=e._w,l=n._x,c=n._y,u=n._z,h=n._w;return this._x=r*h+a*l+s*u-o*c,this._y=s*h+a*c+o*l-r*u,this._z=o*h+a*u+r*c-s*l,this._w=a*h-r*l-s*c-o*u,this._onChangeCallback(),this}slerp(e,n){let r=e._x,s=e._y,o=e._z,a=e._w,l=this.dot(e);l<0&&(r=-r,s=-s,o=-o,a=-a,l=-l);let c=1-n;if(l<.9995){let u=Math.acos(l),h=Math.sin(u);c=Math.sin(c*u)/h,n=Math.sin(n*u)/h,this._x=this._x*c+r*n,this._y=this._y*c+s*n,this._z=this._z*c+o*n,this._w=this._w*c+a*n,this._onChangeCallback()}else this._x=this._x*c+r*n,this._y=this._y*c+s*n,this._z=this._z*c+o*n,this._w=this._w*c+a*n,this.normalize();return this}slerpQuaternions(e,n,r){return this.copy(e).slerp(n,r)}random(){let e=2*Math.PI*Math.random(),n=2*Math.PI*Math.random(),r=Math.random(),s=Math.sqrt(1-r),o=Math.sqrt(r);return this.set(s*Math.sin(e),s*Math.cos(e),o*Math.sin(n),o*Math.cos(n))}equals(e){return e._x===this._x&&e._y===this._y&&e._z===this._z&&e._w===this._w}fromArray(e,n=0){return this._x=e[n],this._y=e[n+1],this._z=e[n+2],this._w=e[n+3],this._onChangeCallback(),this}toArray(e=[],n=0){return e[n]=this._x,e[n+1]=this._y,e[n+2]=this._z,e[n+3]=this._w,e}fromBufferAttribute(e,n){return this._x=e.getX(n),this._y=e.getY(n),this._z=e.getZ(n),this._w=e.getW(n),this._onChangeCallback(),this}toJSON(){return this.toArray()}_onChange(e){return this._onChangeCallback=e,this}_onChangeCallback(){}*[Symbol.iterator](){yield this._x,yield this._y,yield this._z,yield this._w}},jf=class jf{constructor(e=0,n=0,r=0){this.x=e,this.y=n,this.z=r}set(e,n,r){return r===void 0&&(r=this.z),this.x=e,this.y=n,this.z=r,this}setScalar(e){return this.x=e,this.y=e,this.z=e,this}setX(e){return this.x=e,this}setY(e){return this.y=e,this}setZ(e){return this.z=e,this}setComponent(e,n){switch(e){case 0:this.x=n;break;case 1:this.y=n;break;case 2:this.z=n;break;default:throw new Error("THREE.Vector3: index is out of range: "+e)}return this}getComponent(e){switch(e){case 0:return this.x;case 1:return this.y;case 2:return this.z;default:throw new Error("THREE.Vector3: index is out of range: "+e)}}clone(){return new this.constructor(this.x,this.y,this.z)}copy(e){return this.x=e.x,this.y=e.y,this.z=e.z,this}add(e){return this.x+=e.x,this.y+=e.y,this.z+=e.z,this}addScalar(e){return this.x+=e,this.y+=e,this.z+=e,this}addVectors(e,n){return this.x=e.x+n.x,this.y=e.y+n.y,this.z=e.z+n.z,this}addScaledVector(e,n){return this.x+=e.x*n,this.y+=e.y*n,this.z+=e.z*n,this}sub(e){return this.x-=e.x,this.y-=e.y,this.z-=e.z,this}subScalar(e){return this.x-=e,this.y-=e,this.z-=e,this}subVectors(e,n){return this.x=e.x-n.x,this.y=e.y-n.y,this.z=e.z-n.z,this}multiply(e){return this.x*=e.x,this.y*=e.y,this.z*=e.z,this}multiplyScalar(e){return this.x*=e,this.y*=e,this.z*=e,this}multiplyVectors(e,n){return this.x=e.x*n.x,this.y=e.y*n.y,this.z=e.z*n.z,this}applyEuler(e){return this.applyQuaternion(pm.setFromEuler(e))}applyAxisAngle(e,n){return this.applyQuaternion(pm.setFromAxisAngle(e,n))}applyMatrix3(e){let n=this.x,r=this.y,s=this.z,o=e.elements;return this.x=o[0]*n+o[3]*r+o[6]*s,this.y=o[1]*n+o[4]*r+o[7]*s,this.z=o[2]*n+o[5]*r+o[8]*s,this}applyNormalMatrix(e){return this.applyMatrix3(e).normalize()}applyMatrix4(e){let n=this.x,r=this.y,s=this.z,o=e.elements,a=1/(o[3]*n+o[7]*r+o[11]*s+o[15]);return this.x=(o[0]*n+o[4]*r+o[8]*s+o[12])*a,this.y=(o[1]*n+o[5]*r+o[9]*s+o[13])*a,this.z=(o[2]*n+o[6]*r+o[10]*s+o[14])*a,this}applyQuaternion(e){let n=this.x,r=this.y,s=this.z,o=e.x,a=e.y,l=e.z,c=e.w,u=2*(a*s-l*r),h=2*(l*n-o*s),d=2*(o*r-a*n);return this.x=n+c*u+a*d-l*h,this.y=r+c*h+l*u-o*d,this.z=s+c*d+o*h-a*u,this}project(e){return this.applyMatrix4(e.matrixWorldInverse).applyMatrix4(e.projectionMatrix)}unproject(e){return this.applyMatrix4(e.projectionMatrixInverse).applyMatrix4(e.matrixWorld)}transformDirection(e){let n=this.x,r=this.y,s=this.z,o=e.elements;return this.x=o[0]*n+o[4]*r+o[8]*s,this.y=o[1]*n+o[5]*r+o[9]*s,this.z=o[2]*n+o[6]*r+o[10]*s,this.normalize()}divide(e){return this.x/=e.x,this.y/=e.y,this.z/=e.z,this}divideScalar(e){return this.multiplyScalar(1/e)}min(e){return this.x=Math.min(this.x,e.x),this.y=Math.min(this.y,e.y),this.z=Math.min(this.z,e.z),this}max(e){return this.x=Math.max(this.x,e.x),this.y=Math.max(this.y,e.y),this.z=Math.max(this.z,e.z),this}clamp(e,n){return this.x=je(this.x,e.x,n.x),this.y=je(this.y,e.y,n.y),this.z=je(this.z,e.z,n.z),this}clampScalar(e,n){return this.x=je(this.x,e,n),this.y=je(this.y,e,n),this.z=je(this.z,e,n),this}clampLength(e,n){let r=this.length();return this.divideScalar(r||1).multiplyScalar(je(r,e,n))}floor(){return this.x=Math.floor(this.x),this.y=Math.floor(this.y),this.z=Math.floor(this.z),this}ceil(){return this.x=Math.ceil(this.x),this.y=Math.ceil(this.y),this.z=Math.ceil(this.z),this}round(){return this.x=Math.round(this.x),this.y=Math.round(this.y),this.z=Math.round(this.z),this}roundToZero(){return this.x=Math.trunc(this.x),this.y=Math.trunc(this.y),this.z=Math.trunc(this.z),this}negate(){return this.x=-this.x,this.y=-this.y,this.z=-this.z,this}dot(e){return this.x*e.x+this.y*e.y+this.z*e.z}lengthSq(){return this.x*this.x+this.y*this.y+this.z*this.z}length(){return Math.sqrt(this.x*this.x+this.y*this.y+this.z*this.z)}manhattanLength(){return Math.abs(this.x)+Math.abs(this.y)+Math.abs(this.z)}normalize(){return this.divideScalar(this.length()||1)}setLength(e){return this.normalize().multiplyScalar(e)}lerp(e,n){return this.x+=(e.x-this.x)*n,this.y+=(e.y-this.y)*n,this.z+=(e.z-this.z)*n,this}lerpVectors(e,n,r){return this.x=e.x+(n.x-e.x)*r,this.y=e.y+(n.y-e.y)*r,this.z=e.z+(n.z-e.z)*r,this}cross(e){return this.crossVectors(this,e)}crossVectors(e,n){let r=e.x,s=e.y,o=e.z,a=n.x,l=n.y,c=n.z;return this.x=s*c-o*l,this.y=o*a-r*c,this.z=r*l-s*a,this}projectOnVector(e){let n=e.lengthSq();if(n===0)return this.set(0,0,0);let r=e.dot(this)/n;return this.copy(e).multiplyScalar(r)}projectOnPlane(e){return Fh.copy(this).projectOnVector(e),this.sub(Fh)}reflect(e){return this.sub(Fh.copy(e).multiplyScalar(2*this.dot(e)))}angleTo(e){let n=Math.sqrt(this.lengthSq()*e.lengthSq());if(n===0)return Math.PI/2;let r=this.dot(e)/n;return Math.acos(je(r,-1,1))}distanceTo(e){return Math.sqrt(this.distanceToSquared(e))}distanceToSquared(e){let n=this.x-e.x,r=this.y-e.y,s=this.z-e.z;return n*n+r*r+s*s}manhattanDistanceTo(e){return Math.abs(this.x-e.x)+Math.abs(this.y-e.y)+Math.abs(this.z-e.z)}setFromSpherical(e){return this.setFromSphericalCoords(e.radius,e.phi,e.theta)}setFromSphericalCoords(e,n,r){let s=Math.sin(n)*e;return this.x=s*Math.sin(r),this.y=Math.cos(n)*e,this.z=s*Math.cos(r),this}setFromCylindrical(e){return this.setFromCylindricalCoords(e.radius,e.theta,e.y)}setFromCylindricalCoords(e,n,r){return this.x=e*Math.sin(n),this.y=r,this.z=e*Math.cos(n),this}setFromMatrixPosition(e){let n=e.elements;return this.x=n[12],this.y=n[13],this.z=n[14],this}setFromMatrixScale(e){let n=this.setFromMatrixColumn(e,0).length(),r=this.setFromMatrixColumn(e,1).length(),s=this.setFromMatrixColumn(e,2).length();return this.x=n,this.y=r,this.z=s,this}setFromMatrixColumn(e,n){return this.fromArray(e.elements,n*4)}setFromMatrix3Column(e,n){return this.fromArray(e.elements,n*3)}setFromEuler(e){return this.x=e._x,this.y=e._y,this.z=e._z,this}setFromColor(e){return this.x=e.r,this.y=e.g,this.z=e.b,this}equals(e){return e.x===this.x&&e.y===this.y&&e.z===this.z}fromArray(e,n=0){return this.x=e[n],this.y=e[n+1],this.z=e[n+2],this}toArray(e=[],n=0){return e[n]=this.x,e[n+1]=this.y,e[n+2]=this.z,e}fromBufferAttribute(e,n){return this.x=e.getX(n),this.y=e.getY(n),this.z=e.getZ(n),this}random(){return this.x=Math.random(),this.y=Math.random(),this.z=Math.random(),this}randomDirection(){let e=Math.random()*Math.PI*2,n=Math.random()*2-1,r=Math.sqrt(1-n*n);return this.x=r*Math.cos(e),this.y=n,this.z=r*Math.sin(e),this}*[Symbol.iterator](){yield this.x,yield this.y,yield this.z}};jf.prototype.isVector3=!0;var F=jf,Fh=new F,pm=new Ut,Yf=class Yf{constructor(e,n,r,s,o,a,l,c,u){this.elements=[1,0,0,0,1,0,0,0,1],e!==void 0&&this.set(e,n,r,s,o,a,l,c,u)}set(e,n,r,s,o,a,l,c,u){let h=this.elements;return h[0]=e,h[1]=s,h[2]=l,h[3]=n,h[4]=o,h[5]=c,h[6]=r,h[7]=a,h[8]=u,this}identity(){return this.set(1,0,0,0,1,0,0,0,1),this}copy(e){let n=this.elements,r=e.elements;return n[0]=r[0],n[1]=r[1],n[2]=r[2],n[3]=r[3],n[4]=r[4],n[5]=r[5],n[6]=r[6],n[7]=r[7],n[8]=r[8],this}extractBasis(e,n,r){return e.setFromMatrix3Column(this,0),n.setFromMatrix3Column(this,1),r.setFromMatrix3Column(this,2),this}setFromMatrix4(e){let n=e.elements;return this.set(n[0],n[4],n[8],n[1],n[5],n[9],n[2],n[6],n[10]),this}multiply(e){return this.multiplyMatrices(this,e)}premultiply(e){return this.multiplyMatrices(e,this)}multiplyMatrices(e,n){let r=e.elements,s=n.elements,o=this.elements,a=r[0],l=r[3],c=r[6],u=r[1],h=r[4],d=r[7],f=r[2],p=r[5],g=r[8],v=s[0],_=s[3],m=s[6],w=s[1],T=s[4],y=s[7],b=s[2],S=s[5],A=s[8];return o[0]=a*v+l*w+c*b,o[3]=a*_+l*T+c*S,o[6]=a*m+l*y+c*A,o[1]=u*v+h*w+d*b,o[4]=u*_+h*T+d*S,o[7]=u*m+h*y+d*A,o[2]=f*v+p*w+g*b,o[5]=f*_+p*T+g*S,o[8]=f*m+p*y+g*A,this}multiplyScalar(e){let n=this.elements;return n[0]*=e,n[3]*=e,n[6]*=e,n[1]*=e,n[4]*=e,n[7]*=e,n[2]*=e,n[5]*=e,n[8]*=e,this}determinant(){let e=this.elements,n=e[0],r=e[1],s=e[2],o=e[3],a=e[4],l=e[5],c=e[6],u=e[7],h=e[8];return n*a*h-n*l*u-r*o*h+r*l*c+s*o*u-s*a*c}invert(){let e=this.elements,n=e[0],r=e[1],s=e[2],o=e[3],a=e[4],l=e[5],c=e[6],u=e[7],h=e[8],d=h*a-l*u,f=l*c-h*o,p=u*o-a*c,g=n*d+r*f+s*p;if(g===0)return this.set(0,0,0,0,0,0,0,0,0);let v=1/g;return e[0]=d*v,e[1]=(s*u-h*r)*v,e[2]=(l*r-s*a)*v,e[3]=f*v,e[4]=(h*n-s*c)*v,e[5]=(s*o-l*n)*v,e[6]=p*v,e[7]=(r*c-u*n)*v,e[8]=(a*n-r*o)*v,this}transpose(){let e,n=this.elements;return e=n[1],n[1]=n[3],n[3]=e,e=n[2],n[2]=n[6],n[6]=e,e=n[5],n[5]=n[7],n[7]=e,this}getNormalMatrix(e){return this.setFromMatrix4(e).invert().transpose()}transposeIntoArray(e){let n=this.elements;return e[0]=n[0],e[1]=n[3],e[2]=n[6],e[3]=n[1],e[4]=n[4],e[5]=n[7],e[6]=n[2],e[7]=n[5],e[8]=n[8],this}setUvTransform(e,n,r,s,o,a,l){let c=Math.cos(o),u=Math.sin(o);return this.set(r*c,r*u,-r*(c*a+u*l)+a+e,-s*u,s*c,-s*(-u*a+c*l)+l+n,0,0,1),this}scale(e,n){return Rr("Matrix3: .scale() is deprecated. Use .makeScale() instead."),this.premultiply(kh.makeScale(e,n)),this}rotate(e){return Rr("Matrix3: .rotate() is deprecated. Use .makeRotation() instead."),this.premultiply(kh.makeRotation(-e)),this}translate(e,n){return Rr("Matrix3: .translate() is deprecated. Use .makeTranslation() instead."),this.premultiply(kh.makeTranslation(e,n)),this}makeTranslation(e,n){return e.isVector2?this.set(1,0,e.x,0,1,e.y,0,0,1):this.set(1,0,e,0,1,n,0,0,1),this}makeRotation(e){let n=Math.cos(e),r=Math.sin(e);return this.set(n,-r,0,r,n,0,0,0,1),this}makeScale(e,n){return this.set(e,0,0,0,n,0,0,0,1),this}equals(e){let n=this.elements,r=e.elements;for(let s=0;s<9;s++)if(n[s]!==r[s])return!1;return!0}fromArray(e,n=0){for(let r=0;r<9;r++)this.elements[r]=e[r+n];return this}toArray(e=[],n=0){let r=this.elements;return e[n]=r[0],e[n+1]=r[1],e[n+2]=r[2],e[n+3]=r[3],e[n+4]=r[4],e[n+5]=r[5],e[n+6]=r[6],e[n+7]=r[7],e[n+8]=r[8],e}clone(){return new this.constructor().fromArray(this.elements)}};Yf.prototype.isMatrix3=!0;var He=Yf,kh=new He,mm=new He().set(.4123908,.3575843,.1804808,.212639,.7151687,.0721923,.0193308,.1191948,.9505322),gm=new He().set(3.2409699,-1.5373832,-.4986108,-.9692436,1.8759675,.0415551,.0556301,-.203977,1.0569715);function ES(){let i={enabled:!0,workingColorSpace:wo,spaces:{},convert:function(s,o,a){return this.enabled===!1||o===a||!o||!a||(this.spaces[o].transfer===rt&&(s.r=Ti(s.r),s.g=Ti(s.g),s.b=Ti(s.b)),this.spaces[o].primaries!==this.spaces[a].primaries&&(s.applyMatrix3(this.spaces[o].toXYZ),s.applyMatrix3(this.spaces[a].fromXYZ)),this.spaces[a].transfer===rt&&(s.r=bs(s.r),s.g=bs(s.g),s.b=bs(s.b))),s},workingToColorSpace:function(s,o){return this.convert(s,this.workingColorSpace,o)},colorSpaceToWorking:function(s,o){return this.convert(s,o,this.workingColorSpace)},getPrimaries:function(s){return this.spaces[s].primaries},getTransfer:function(s){return s===Pi?Mo:this.spaces[s].transfer},getToneMappingMode:function(s){return this.spaces[s].outputColorSpaceConfig.toneMappingMode||"standard"},getLuminanceCoefficients:function(s,o=this.workingColorSpace){return s.fromArray(this.spaces[o].luminanceCoefficients)},define:function(s){Object.assign(this.spaces,s)},_getMatrix:function(s,o,a){return s.copy(this.spaces[o].toXYZ).multiply(this.spaces[a].fromXYZ)},_getDrawingBufferColorSpace:function(s){return this.spaces[s].outputColorSpaceConfig.drawingBufferColorSpace},_getUnpackColorSpace:function(s=this.workingColorSpace){return this.spaces[s].workingColorSpaceConfig.unpackColorSpace},fromWorkingColorSpace:function(s,o){return Rr("ColorManagement: .fromWorkingColorSpace() has been renamed to .workingToColorSpace()."),i.workingToColorSpace(s,o)},toWorkingColorSpace:function(s,o){return Rr("ColorManagement: .toWorkingColorSpace() has been renamed to .colorSpaceToWorking()."),i.colorSpaceToWorking(s,o)}},e=[.64,.33,.3,.6,.15,.06],n=[.2126,.7152,.0722],r=[.3127,.329];return i.define({[wo]:{primaries:e,whitePoint:r,transfer:Mo,toXYZ:mm,fromXYZ:gm,luminanceCoefficients:n,workingColorSpaceConfig:{unpackColorSpace:on},outputColorSpaceConfig:{drawingBufferColorSpace:on}},[on]:{primaries:e,whitePoint:r,transfer:rt,toXYZ:mm,fromXYZ:gm,luminanceCoefficients:n,outputColorSpaceConfig:{drawingBufferColorSpace:on}}}),i}var Ke=ES();function Ti(i){return i<.04045?i*.0773993808:Math.pow(i*.9478672986+.0521327014,2.4)}function bs(i){return i<.0031308?i*12.92:1.055*Math.pow(i,.41666)-.055}var os,Ol=class{static getDataURL(e,n="image/png"){if(/^data:/i.test(e.src)||typeof HTMLCanvasElement>"u")return e.src;let r;if(e instanceof HTMLCanvasElement)r=e;else{os===void 0&&(os=Ms("canvas")),os.width=e.width,os.height=e.height;let s=os.getContext("2d");e instanceof ImageData?s.putImageData(e,0,0):s.drawImage(e,0,0,e.width,e.height),r=os}return r.toDataURL(n)}static sRGBToLinear(e){if(typeof HTMLImageElement<"u"&&e instanceof HTMLImageElement||typeof HTMLCanvasElement<"u"&&e instanceof HTMLCanvasElement||typeof ImageBitmap<"u"&&e instanceof ImageBitmap){let n=Ms("canvas");n.width=e.width,n.height=e.height;let r=n.getContext("2d");r.drawImage(e,0,0,e.width,e.height);let s=r.getImageData(0,0,e.width,e.height),o=s.data;for(let a=0;a<o.length;a++)o[a]=Ti(o[a]/255)*255;return r.putImageData(s,0,0),n}else if(e.data){let n=e.data.slice(0);for(let r=0;r<n.length;r++)n instanceof Uint8Array||n instanceof Uint8ClampedArray?n[r]=Math.floor(Ti(n[r]/255)*255):n[r]=Ti(n[r]);return{data:n,width:e.width,height:e.height}}else return Ue("ImageUtils.sRGBToLinear(): Unsupported image type. No color space conversion applied."),e}},AS=0,Ts=class{constructor(e=null){this.isTextureSource=!0,Object.defineProperty(this,"id",{value:AS++}),this.uuid=Vs(),this.data=e,this.dataReady=!0,this.version=0}getSize(e){let n=this.data;return typeof HTMLVideoElement<"u"&&n instanceof HTMLVideoElement?e.set(n.videoWidth,n.videoHeight,0):typeof VideoFrame<"u"&&n instanceof VideoFrame?e.set(n.displayWidth,n.displayHeight,0):n!==null?e.set(n.width,n.height,n.depth||0):e.set(0,0,0),e}set needsUpdate(e){e===!0&&this.version++}toJSON(e){let n=e===void 0||typeof e=="string";if(!n&&e.images[this.uuid]!==void 0)return e.images[this.uuid];let r={uuid:this.uuid,url:""},s=this.data;if(s!==null){let o;if(Array.isArray(s)){o=[];for(let a=0,l=s.length;a<l;a++)s[a].isDataTexture?o.push(Bh(s[a].image)):o.push(Bh(s[a]))}else o=Bh(s);r.url=o}return n||(e.images[this.uuid]=r),r}};function Bh(i){return typeof HTMLImageElement<"u"&&i instanceof HTMLImageElement||typeof HTMLCanvasElement<"u"&&i instanceof HTMLCanvasElement||typeof ImageBitmap<"u"&&i instanceof ImageBitmap?Ol.getDataURL(i):i.data?{data:Array.from(i.data),width:i.width,height:i.height,type:i.data.constructor.name}:(Ue("Texture: Unable to serialize Texture."),{})}var TS=0,zh=new F,ln=class i extends Yn{constructor(e=i.DEFAULT_IMAGE,n=i.DEFAULT_MAPPING,r=ui,s=ui,o=Vt,a=sr,l=Nn,c=mn,u=i.DEFAULT_ANISOTROPY,h=Pi){super(),this.isTexture=!0,Object.defineProperty(this,"id",{value:TS++}),this.uuid=Vs(),this.name="",this.source=new Ts(e),this.mipmaps=[],this.mapping=n,this.channel=0,this.wrapS=r,this.wrapT=s,this.magFilter=o,this.minFilter=a,this.anisotropy=u,this.format=l,this.internalFormat=null,this.type=c,this.offset=new fe(0,0),this.repeat=new fe(1,1),this.center=new fe(0,0),this.rotation=0,this.matrixAutoUpdate=!0,this.matrix=new He,this.generateMipmaps=!0,this.premultiplyAlpha=!1,this.flipY=!0,this.unpackAlignment=4,this.colorSpace=h,this.userData={},this.updateRanges=[],this.version=0,this.onUpdate=null,this.renderTarget=null,this.isRenderTargetTexture=!1,this.isArrayTexture=!!(e&&e.depth&&e.depth>1),this.pmremVersion=0,this.normalized=!1}get width(){return this.source.getSize(zh).x}get height(){return this.source.getSize(zh).y}get depth(){return this.source.getSize(zh).z}get image(){return this.source.data}set image(e){this.source.data=e}updateMatrix(){this.matrix.setUvTransform(this.offset.x,this.offset.y,this.repeat.x,this.repeat.y,this.rotation,this.center.x,this.center.y)}addUpdateRange(e,n){this.updateRanges.push({start:e,count:n})}clearUpdateRanges(){this.updateRanges.length=0}clone(){return new this.constructor().copy(this)}copy(e){return this.name=e.name,this.source=e.source,this.mipmaps=e.mipmaps.slice(0),this.mapping=e.mapping,this.channel=e.channel,this.wrapS=e.wrapS,this.wrapT=e.wrapT,this.magFilter=e.magFilter,this.minFilter=e.minFilter,this.anisotropy=e.anisotropy,this.format=e.format,this.internalFormat=e.internalFormat,this.type=e.type,this.normalized=e.normalized,this.offset.copy(e.offset),this.repeat.copy(e.repeat),this.center.copy(e.center),this.rotation=e.rotation,this.matrixAutoUpdate=e.matrixAutoUpdate,this.matrix.copy(e.matrix),this.generateMipmaps=e.generateMipmaps,this.premultiplyAlpha=e.premultiplyAlpha,this.flipY=e.flipY,this.unpackAlignment=e.unpackAlignment,this.colorSpace=e.colorSpace,this.renderTarget=e.renderTarget,this.isRenderTargetTexture=e.isRenderTargetTexture,this.isArrayTexture=e.isArrayTexture,this.userData=JSON.parse(JSON.stringify(e.userData)),this.needsUpdate=!0,this}setValues(e){for(let n in e){let r=e[n];if(r===void 0){Ue(`Texture.setValues(): parameter '${n}' has value of undefined.`);continue}let s=this[n];if(s===void 0){Ue(`Texture.setValues(): property '${n}' does not exist.`);continue}s&&r&&s.isVector2&&r.isVector2||s&&r&&s.isVector3&&r.isVector3||s&&r&&s.isMatrix3&&r.isMatrix3?s.copy(r):this[n]=r}}toJSON(e){let n=e===void 0||typeof e=="string";if(!n&&e.textures[this.uuid]!==void 0)return e.textures[this.uuid];let r={metadata:{version:4.7,type:"Texture",generator:"Texture.toJSON"},uuid:this.uuid,name:this.name,image:this.source.toJSON(e).uuid,mapping:this.mapping,channel:this.channel,repeat:[this.repeat.x,this.repeat.y],offset:[this.offset.x,this.offset.y],center:[this.center.x,this.center.y],rotation:this.rotation,wrap:[this.wrapS,this.wrapT],format:this.format,internalFormat:this.internalFormat,type:this.type,normalized:this.normalized,colorSpace:this.colorSpace,minFilter:this.minFilter,magFilter:this.magFilter,anisotropy:this.anisotropy,flipY:this.flipY,generateMipmaps:this.generateMipmaps,premultiplyAlpha:this.premultiplyAlpha,unpackAlignment:this.unpackAlignment};return Object.keys(this.userData).length>0&&(r.userData=this.userData),n||(e.textures[this.uuid]=r),r}dispose(){this.dispatchEvent({type:"dispose"})}transformUv(e){if(this.mapping!==Pf)return e;if(e.applyMatrix3(this.matrix),e.x<0||e.x>1)switch(this.wrapS){case Il:e.x=e.x-Math.floor(e.x);break;case ui:e.x=e.x<0?0:1;break;case Ll:Math.abs(Math.floor(e.x)%2)===1?e.x=Math.ceil(e.x)-e.x:e.x=e.x-Math.floor(e.x);break}if(e.y<0||e.y>1)switch(this.wrapT){case Il:e.y=e.y-Math.floor(e.y);break;case ui:e.y=e.y<0?0:1;break;case Ll:Math.abs(Math.floor(e.y)%2)===1?e.y=Math.ceil(e.y)-e.y:e.y=e.y-Math.floor(e.y);break}return this.flipY&&(e.y=1-e.y),e}set needsUpdate(e){e===!0&&(this.version++,this.source.needsUpdate=!0)}set needsPMREMUpdate(e){e===!0&&this.pmremVersion++}};ln.DEFAULT_IMAGE=null;ln.DEFAULT_MAPPING=Pf;ln.DEFAULT_ANISOTROPY=1;var $f=class $f{constructor(e=0,n=0,r=0,s=1){this.x=e,this.y=n,this.z=r,this.w=s}get width(){return this.z}set width(e){this.z=e}get height(){return this.w}set height(e){this.w=e}set(e,n,r,s){return this.x=e,this.y=n,this.z=r,this.w=s,this}setScalar(e){return this.x=e,this.y=e,this.z=e,this.w=e,this}setX(e){return this.x=e,this}setY(e){return this.y=e,this}setZ(e){return this.z=e,this}setW(e){return this.w=e,this}setComponent(e,n){switch(e){case 0:this.x=n;break;case 1:this.y=n;break;case 2:this.z=n;break;case 3:this.w=n;break;default:throw new Error("THREE.Vector4: index is out of range: "+e)}return this}getComponent(e){switch(e){case 0:return this.x;case 1:return this.y;case 2:return this.z;case 3:return this.w;default:throw new Error("THREE.Vector4: index is out of range: "+e)}}clone(){return new this.constructor(this.x,this.y,this.z,this.w)}copy(e){return this.x=e.x,this.y=e.y,this.z=e.z,this.w=e.w!==void 0?e.w:1,this}add(e){return this.x+=e.x,this.y+=e.y,this.z+=e.z,this.w+=e.w,this}addScalar(e){return this.x+=e,this.y+=e,this.z+=e,this.w+=e,this}addVectors(e,n){return this.x=e.x+n.x,this.y=e.y+n.y,this.z=e.z+n.z,this.w=e.w+n.w,this}addScaledVector(e,n){return this.x+=e.x*n,this.y+=e.y*n,this.z+=e.z*n,this.w+=e.w*n,this}sub(e){return this.x-=e.x,this.y-=e.y,this.z-=e.z,this.w-=e.w,this}subScalar(e){return this.x-=e,this.y-=e,this.z-=e,this.w-=e,this}subVectors(e,n){return this.x=e.x-n.x,this.y=e.y-n.y,this.z=e.z-n.z,this.w=e.w-n.w,this}multiply(e){return this.x*=e.x,this.y*=e.y,this.z*=e.z,this.w*=e.w,this}multiplyScalar(e){return this.x*=e,this.y*=e,this.z*=e,this.w*=e,this}applyMatrix4(e){let n=this.x,r=this.y,s=this.z,o=this.w,a=e.elements;return this.x=a[0]*n+a[4]*r+a[8]*s+a[12]*o,this.y=a[1]*n+a[5]*r+a[9]*s+a[13]*o,this.z=a[2]*n+a[6]*r+a[10]*s+a[14]*o,this.w=a[3]*n+a[7]*r+a[11]*s+a[15]*o,this}divide(e){return this.x/=e.x,this.y/=e.y,this.z/=e.z,this.w/=e.w,this}divideScalar(e){return this.multiplyScalar(1/e)}setAxisAngleFromQuaternion(e){this.w=2*Math.acos(e.w);let n=Math.sqrt(1-e.w*e.w);return n<1e-4?(this.x=1,this.y=0,this.z=0):(this.x=e.x/n,this.y=e.y/n,this.z=e.z/n),this}setAxisAngleFromRotationMatrix(e){let n,r,s,o,c=e.elements,u=c[0],h=c[4],d=c[8],f=c[1],p=c[5],g=c[9],v=c[2],_=c[6],m=c[10];if(Math.abs(h-f)<.01&&Math.abs(d-v)<.01&&Math.abs(g-_)<.01){if(Math.abs(h+f)<.1&&Math.abs(d+v)<.1&&Math.abs(g+_)<.1&&Math.abs(u+p+m-3)<.1)return this.set(1,0,0,0),this;n=Math.PI;let T=(u+1)/2,y=(p+1)/2,b=(m+1)/2,S=(h+f)/4,A=(d+v)/4,x=(g+_)/4;return T>y&&T>b?T<.01?(r=0,s=.707106781,o=.707106781):(r=Math.sqrt(T),s=S/r,o=A/r):y>b?y<.01?(r=.707106781,s=0,o=.707106781):(s=Math.sqrt(y),r=S/s,o=x/s):b<.01?(r=.707106781,s=.707106781,o=0):(o=Math.sqrt(b),r=A/o,s=x/o),this.set(r,s,o,n),this}let w=Math.sqrt((_-g)*(_-g)+(d-v)*(d-v)+(f-h)*(f-h));return Math.abs(w)<.001&&(w=1),this.x=(_-g)/w,this.y=(d-v)/w,this.z=(f-h)/w,this.w=Math.acos((u+p+m-1)/2),this}setFromMatrixPosition(e){let n=e.elements;return this.x=n[12],this.y=n[13],this.z=n[14],this.w=n[15],this}min(e){return this.x=Math.min(this.x,e.x),this.y=Math.min(this.y,e.y),this.z=Math.min(this.z,e.z),this.w=Math.min(this.w,e.w),this}max(e){return this.x=Math.max(this.x,e.x),this.y=Math.max(this.y,e.y),this.z=Math.max(this.z,e.z),this.w=Math.max(this.w,e.w),this}clamp(e,n){return this.x=je(this.x,e.x,n.x),this.y=je(this.y,e.y,n.y),this.z=je(this.z,e.z,n.z),this.w=je(this.w,e.w,n.w),this}clampScalar(e,n){return this.x=je(this.x,e,n),this.y=je(this.y,e,n),this.z=je(this.z,e,n),this.w=je(this.w,e,n),this}clampLength(e,n){let r=this.length();return this.divideScalar(r||1).multiplyScalar(je(r,e,n))}floor(){return this.x=Math.floor(this.x),this.y=Math.floor(this.y),this.z=Math.floor(this.z),this.w=Math.floor(this.w),this}ceil(){return this.x=Math.ceil(this.x),this.y=Math.ceil(this.y),this.z=Math.ceil(this.z),this.w=Math.ceil(this.w),this}round(){return this.x=Math.round(this.x),this.y=Math.round(this.y),this.z=Math.round(this.z),this.w=Math.round(this.w),this}roundToZero(){return this.x=Math.trunc(this.x),this.y=Math.trunc(this.y),this.z=Math.trunc(this.z),this.w=Math.trunc(this.w),this}negate(){return this.x=-this.x,this.y=-this.y,this.z=-this.z,this.w=-this.w,this}dot(e){return this.x*e.x+this.y*e.y+this.z*e.z+this.w*e.w}lengthSq(){return this.x*this.x+this.y*this.y+this.z*this.z+this.w*this.w}length(){return Math.sqrt(this.x*this.x+this.y*this.y+this.z*this.z+this.w*this.w)}manhattanLength(){return Math.abs(this.x)+Math.abs(this.y)+Math.abs(this.z)+Math.abs(this.w)}normalize(){return this.divideScalar(this.length()||1)}setLength(e){return this.normalize().multiplyScalar(e)}lerp(e,n){return this.x+=(e.x-this.x)*n,this.y+=(e.y-this.y)*n,this.z+=(e.z-this.z)*n,this.w+=(e.w-this.w)*n,this}lerpVectors(e,n,r){return this.x=e.x+(n.x-e.x)*r,this.y=e.y+(n.y-e.y)*r,this.z=e.z+(n.z-e.z)*r,this.w=e.w+(n.w-e.w)*r,this}equals(e){return e.x===this.x&&e.y===this.y&&e.z===this.z&&e.w===this.w}fromArray(e,n=0){return this.x=e[n],this.y=e[n+1],this.z=e[n+2],this.w=e[n+3],this}toArray(e=[],n=0){return e[n]=this.x,e[n+1]=this.y,e[n+2]=this.z,e[n+3]=this.w,e}fromBufferAttribute(e,n){return this.x=e.getX(n),this.y=e.getY(n),this.z=e.getZ(n),this.w=e.getW(n),this}random(){return this.x=Math.random(),this.y=Math.random(),this.z=Math.random(),this.w=Math.random(),this}*[Symbol.iterator](){yield this.x,yield this.y,yield this.z,yield this.w}};$f.prototype.isVector4=!0;var vt=$f,Nl=class extends Yn{constructor(e=1,n=1,r={}){super(),r=Object.assign({generateMipmaps:!1,internalFormat:null,minFilter:Vt,depthBuffer:!0,stencilBuffer:!1,resolveColorBuffer:!0,resolveDepthBuffer:!0,resolveStencilBuffer:!0,storeMultisampledColorBuffer:!0,storeMultisampledDepthBuffer:!0,storeMultisampledStencilBuffer:!0,depthTexture:null,samples:0,count:1,depth:1,multiview:!1,useArrayDepthTexture:!1},r),this.isRenderTarget=!0,this.width=e,this.height=n,this.depth=r.depth,this.scissor=new vt(0,0,e,n),this.scissorTest=!1,this.viewport=new vt(0,0,e,n),this.textures=[];let s={width:e,height:n,depth:r.depth},o=new ln(s),a=r.count;for(let l=0;l<a;l++)this.textures[l]=o.clone(),this.textures[l].isRenderTargetTexture=!0,this.textures[l].renderTarget=this;this._setTextureOptions(r),this.depthBuffer=r.depthBuffer,this.stencilBuffer=r.stencilBuffer,this.resolveColorBuffer=r.resolveColorBuffer,this.resolveDepthBuffer=r.resolveDepthBuffer,this.resolveStencilBuffer=r.resolveStencilBuffer,this.storeMultisampledColorBuffer=r.storeMultisampledColorBuffer,this.storeMultisampledDepthBuffer=r.storeMultisampledDepthBuffer,this.storeMultisampledStencilBuffer=r.storeMultisampledStencilBuffer,this._depthTexture=null,this.depthTexture=r.depthTexture,this.samples=r.samples,this.multiview=r.multiview,this.useArrayDepthTexture=r.useArrayDepthTexture}_setTextureOptions(e={}){let n={minFilter:Vt,generateMipmaps:!1,flipY:!1,internalFormat:null};e.mapping!==void 0&&(n.mapping=e.mapping),e.wrapS!==void 0&&(n.wrapS=e.wrapS),e.wrapT!==void 0&&(n.wrapT=e.wrapT),e.wrapR!==void 0&&(n.wrapR=e.wrapR),e.magFilter!==void 0&&(n.magFilter=e.magFilter),e.minFilter!==void 0&&(n.minFilter=e.minFilter),e.format!==void 0&&(n.format=e.format),e.type!==void 0&&(n.type=e.type),e.anisotropy!==void 0&&(n.anisotropy=e.anisotropy),e.colorSpace!==void 0&&(n.colorSpace=e.colorSpace),e.flipY!==void 0&&(n.flipY=e.flipY),e.generateMipmaps!==void 0&&(n.generateMipmaps=e.generateMipmaps),e.internalFormat!==void 0&&(n.internalFormat=e.internalFormat);for(let r=0;r<this.textures.length;r++)this.textures[r].setValues(n)}get texture(){return this.textures[0]}set texture(e){this.textures[0]=e}set depthTexture(e){this._depthTexture!==null&&this._depthTexture.renderTarget===this&&(this._depthTexture.renderTarget=null),e!==null&&e.renderTarget===null&&(e.renderTarget=this),this._depthTexture=e}get depthTexture(){return this._depthTexture}setSize(e,n,r=1){if(this.width!==e||this.height!==n||this.depth!==r){this.width=e,this.height=n,this.depth=r;for(let s=0,o=this.textures.length;s<o;s++)this.textures[s].image.width=e,this.textures[s].image.height=n,this.textures[s].image.depth=r,this.textures[s].isData3DTexture!==!0&&(this.textures[s].isArrayTexture=this.textures[s].image.depth>1);this.dispose()}this.viewport.set(0,0,e,n),this.scissor.set(0,0,e,n)}clone(){return new this.constructor().copy(this)}copy(e){this.width=e.width,this.height=e.height,this.depth=e.depth,this.scissor.copy(e.scissor),this.scissorTest=e.scissorTest,this.viewport.copy(e.viewport),this.textures.length=0;for(let n=0,r=e.textures.length;n<r;n++){this.textures[n]=e.textures[n].clone(),this.textures[n].isRenderTargetTexture=!0,this.textures[n].renderTarget=this;let s=Object.assign({},e.textures[n].image);this.textures[n].source=new Ts(s)}if(this.depthBuffer=e.depthBuffer,this.stencilBuffer=e.stencilBuffer,this.resolveColorBuffer=e.resolveColorBuffer,this.resolveDepthBuffer=e.resolveDepthBuffer,this.resolveStencilBuffer=e.resolveStencilBuffer,this.storeMultisampledColorBuffer=e.storeMultisampledColorBuffer,this.storeMultisampledDepthBuffer=e.storeMultisampledDepthBuffer,this.storeMultisampledStencilBuffer=e.storeMultisampledStencilBuffer,e.depthTexture!==null)if(e.depthTexture.renderTarget===e){let n=e.depthTexture.clone();n.renderTarget=null,this.depthTexture=n}else this.depthTexture=e.depthTexture;return this.samples=e.samples,this.multiview=e.multiview,this.useArrayDepthTexture=e.useArrayDepthTexture,this}dispose(){this.dispatchEvent({type:"dispose"})}},Zt=class extends Nl{constructor(e=1,n=1,r={}){super(e,n,r),this.isWebGLRenderTarget=!0}},Eo=class extends ln{constructor(e=null,n=1,r=1,s=1){super(null),this.isDataArrayTexture=!0,this.image={data:e,width:n,height:r,depth:s},this.magFilter=Nt,this.minFilter=Nt,this.wrapR=ui,this.generateMipmaps=!1,this.flipY=!1,this.unpackAlignment=1,this.layerUpdates=new Set}copy(e){return super.copy(e),this.wrapR=e.wrapR,this}addLayerUpdate(e){this.layerUpdates.add(e)}clearLayerUpdates(){this.layerUpdates.clear()}};var Ul=class extends ln{constructor(e=null,n=1,r=1,s=1){super(null),this.isData3DTexture=!0,this.image={data:e,width:n,height:r,depth:s},this.magFilter=Nt,this.minFilter=Nt,this.wrapR=ui,this.generateMipmaps=!1,this.flipY=!1,this.unpackAlignment=1}copy(e){return super.copy(e),this.wrapR=e.wrapR,this}};var uc=class uc{constructor(e,n,r,s,o,a,l,c,u,h,d,f,p,g,v,_){this.elements=[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],e!==void 0&&this.set(e,n,r,s,o,a,l,c,u,h,d,f,p,g,v,_)}set(e,n,r,s,o,a,l,c,u,h,d,f,p,g,v,_){let m=this.elements;return m[0]=e,m[4]=n,m[8]=r,m[12]=s,m[1]=o,m[5]=a,m[9]=l,m[13]=c,m[2]=u,m[6]=h,m[10]=d,m[14]=f,m[3]=p,m[7]=g,m[11]=v,m[15]=_,this}identity(){return this.set(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1),this}clone(){return new uc().fromArray(this.elements)}copy(e){let n=this.elements,r=e.elements;return n[0]=r[0],n[1]=r[1],n[2]=r[2],n[3]=r[3],n[4]=r[4],n[5]=r[5],n[6]=r[6],n[7]=r[7],n[8]=r[8],n[9]=r[9],n[10]=r[10],n[11]=r[11],n[12]=r[12],n[13]=r[13],n[14]=r[14],n[15]=r[15],this}copyPosition(e){let n=this.elements,r=e.elements;return n[12]=r[12],n[13]=r[13],n[14]=r[14],this}setFromMatrix3(e){let n=e.elements;return this.set(n[0],n[3],n[6],0,n[1],n[4],n[7],0,n[2],n[5],n[8],0,0,0,0,1),this}extractBasis(e,n,r){return this.determinantAffine()===0?(e.set(1,0,0),n.set(0,1,0),r.set(0,0,1),this):(e.setFromMatrixColumn(this,0),n.setFromMatrixColumn(this,1),r.setFromMatrixColumn(this,2),this)}makeBasis(e,n,r){return this.set(e.x,n.x,r.x,0,e.y,n.y,r.y,0,e.z,n.z,r.z,0,0,0,0,1),this}extractRotation(e){if(e.determinantAffine()===0)return this.identity();let n=this.elements,r=e.elements,s=1/as.setFromMatrixColumn(e,0).length(),o=1/as.setFromMatrixColumn(e,1).length(),a=1/as.setFromMatrixColumn(e,2).length();return n[0]=r[0]*s,n[1]=r[1]*s,n[2]=r[2]*s,n[3]=0,n[4]=r[4]*o,n[5]=r[5]*o,n[6]=r[6]*o,n[7]=0,n[8]=r[8]*a,n[9]=r[9]*a,n[10]=r[10]*a,n[11]=0,n[12]=0,n[13]=0,n[14]=0,n[15]=1,this}makeRotationFromEuler(e){let n=this.elements,r=e.x,s=e.y,o=e.z,a=Math.cos(r),l=Math.sin(r),c=Math.cos(s),u=Math.sin(s),h=Math.cos(o),d=Math.sin(o);if(e.order==="XYZ"){let f=a*h,p=a*d,g=l*h,v=l*d;n[0]=c*h,n[4]=-c*d,n[8]=u,n[1]=p+g*u,n[5]=f-v*u,n[9]=-l*c,n[2]=v-f*u,n[6]=g+p*u,n[10]=a*c}else if(e.order==="YXZ"){let f=c*h,p=c*d,g=u*h,v=u*d;n[0]=f+v*l,n[4]=g*l-p,n[8]=a*u,n[1]=a*d,n[5]=a*h,n[9]=-l,n[2]=p*l-g,n[6]=v+f*l,n[10]=a*c}else if(e.order==="ZXY"){let f=c*h,p=c*d,g=u*h,v=u*d;n[0]=f-v*l,n[4]=-a*d,n[8]=g+p*l,n[1]=p+g*l,n[5]=a*h,n[9]=v-f*l,n[2]=-a*u,n[6]=l,n[10]=a*c}else if(e.order==="ZYX"){let f=a*h,p=a*d,g=l*h,v=l*d;n[0]=c*h,n[4]=g*u-p,n[8]=f*u+v,n[1]=c*d,n[5]=v*u+f,n[9]=p*u-g,n[2]=-u,n[6]=l*c,n[10]=a*c}else if(e.order==="YZX"){let f=a*c,p=a*u,g=l*c,v=l*u;n[0]=c*h,n[4]=v-f*d,n[8]=g*d+p,n[1]=d,n[5]=a*h,n[9]=-l*h,n[2]=-u*h,n[6]=p*d+g,n[10]=f-v*d}else if(e.order==="XZY"){let f=a*c,p=a*u,g=l*c,v=l*u;n[0]=c*h,n[4]=-d,n[8]=u*h,n[1]=f*d+v,n[5]=a*h,n[9]=p*d-g,n[2]=g*d-p,n[6]=l*h,n[10]=v*d+f}return n[3]=0,n[7]=0,n[11]=0,n[12]=0,n[13]=0,n[14]=0,n[15]=1,this}makeRotationFromQuaternion(e){return this.compose(CS,e,RS)}lookAt(e,n,r){let s=this.elements;return Sn.subVectors(e,n),Sn.lengthSq()===0&&(Sn.z=1),Sn.normalize(),Xi.crossVectors(r,Sn),Xi.lengthSq()===0&&(Math.abs(r.z)===1?Sn.x+=1e-4:Sn.z+=1e-4,Sn.normalize(),Xi.crossVectors(r,Sn)),Xi.normalize(),el.crossVectors(Sn,Xi),s[0]=Xi.x,s[4]=el.x,s[8]=Sn.x,s[1]=Xi.y,s[5]=el.y,s[9]=Sn.y,s[2]=Xi.z,s[6]=el.z,s[10]=Sn.z,this}multiply(e){return this.multiplyMatrices(this,e)}premultiply(e){return this.multiplyMatrices(e,this)}multiplyMatrices(e,n){let r=e.elements,s=n.elements,o=this.elements,a=r[0],l=r[4],c=r[8],u=r[12],h=r[1],d=r[5],f=r[9],p=r[13],g=r[2],v=r[6],_=r[10],m=r[14],w=r[3],T=r[7],y=r[11],b=r[15],S=s[0],A=s[4],x=s[8],C=s[12],L=s[1],P=s[5],O=s[9],U=s[13],M=s[2],I=s[6],D=s[10],k=s[14],q=s[3],W=s[7],j=s[11],B=s[15];return o[0]=a*S+l*L+c*M+u*q,o[4]=a*A+l*P+c*I+u*W,o[8]=a*x+l*O+c*D+u*j,o[12]=a*C+l*U+c*k+u*B,o[1]=h*S+d*L+f*M+p*q,o[5]=h*A+d*P+f*I+p*W,o[9]=h*x+d*O+f*D+p*j,o[13]=h*C+d*U+f*k+p*B,o[2]=g*S+v*L+_*M+m*q,o[6]=g*A+v*P+_*I+m*W,o[10]=g*x+v*O+_*D+m*j,o[14]=g*C+v*U+_*k+m*B,o[3]=w*S+T*L+y*M+b*q,o[7]=w*A+T*P+y*I+b*W,o[11]=w*x+T*O+y*D+b*j,o[15]=w*C+T*U+y*k+b*B,this}multiplyScalar(e){let n=this.elements;return n[0]*=e,n[4]*=e,n[8]*=e,n[12]*=e,n[1]*=e,n[5]*=e,n[9]*=e,n[13]*=e,n[2]*=e,n[6]*=e,n[10]*=e,n[14]*=e,n[3]*=e,n[7]*=e,n[11]*=e,n[15]*=e,this}determinant(){let e=this.elements,n=e[0],r=e[4],s=e[8],o=e[12],a=e[1],l=e[5],c=e[9],u=e[13],h=e[2],d=e[6],f=e[10],p=e[14],g=e[3],v=e[7],_=e[11],m=e[15],w=c*p-u*f,T=l*p-u*d,y=l*f-c*d,b=a*p-u*h,S=a*f-c*h,A=a*d-l*h;return n*(v*w-_*T+m*y)-r*(g*w-_*b+m*S)+s*(g*T-v*b+m*A)-o*(g*y-v*S+_*A)}determinantAffine(){let e=this.elements,n=e[0],r=e[4],s=e[8],o=e[1],a=e[5],l=e[9],c=e[2],u=e[6],h=e[10];return n*(a*h-l*u)-r*(o*h-l*c)+s*(o*u-a*c)}transpose(){let e=this.elements,n;return n=e[1],e[1]=e[4],e[4]=n,n=e[2],e[2]=e[8],e[8]=n,n=e[6],e[6]=e[9],e[9]=n,n=e[3],e[3]=e[12],e[12]=n,n=e[7],e[7]=e[13],e[13]=n,n=e[11],e[11]=e[14],e[14]=n,this}setPosition(e,n,r){let s=this.elements;return e.isVector3?(s[12]=e.x,s[13]=e.y,s[14]=e.z):(s[12]=e,s[13]=n,s[14]=r),this}invert(){let e=this.elements,n=e[0],r=e[1],s=e[2],o=e[3],a=e[4],l=e[5],c=e[6],u=e[7],h=e[8],d=e[9],f=e[10],p=e[11],g=e[12],v=e[13],_=e[14],m=e[15],w=n*l-r*a,T=n*c-s*a,y=n*u-o*a,b=r*c-s*l,S=r*u-o*l,A=s*u-o*c,x=h*v-d*g,C=h*_-f*g,L=h*m-p*g,P=d*_-f*v,O=d*m-p*v,U=f*m-p*_,M=w*U-T*O+y*P+b*L-S*C+A*x;if(M===0)return this.set(0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0);let I=1/M;return e[0]=(l*U-c*O+u*P)*I,e[1]=(s*O-r*U-o*P)*I,e[2]=(v*A-_*S+m*b)*I,e[3]=(f*S-d*A-p*b)*I,e[4]=(c*L-a*U-u*C)*I,e[5]=(n*U-s*L+o*C)*I,e[6]=(_*y-g*A-m*T)*I,e[7]=(h*A-f*y+p*T)*I,e[8]=(a*O-l*L+u*x)*I,e[9]=(r*L-n*O-o*x)*I,e[10]=(g*S-v*y+m*w)*I,e[11]=(d*y-h*S-p*w)*I,e[12]=(l*C-a*P-c*x)*I,e[13]=(n*P-r*C+s*x)*I,e[14]=(v*T-g*b-_*w)*I,e[15]=(h*b-d*T+f*w)*I,this}scale(e){let n=this.elements,r=e.x,s=e.y,o=e.z;return n[0]*=r,n[4]*=s,n[8]*=o,n[1]*=r,n[5]*=s,n[9]*=o,n[2]*=r,n[6]*=s,n[10]*=o,n[3]*=r,n[7]*=s,n[11]*=o,this}getMaxScaleOnAxis(){let e=this.elements,n=e[0]*e[0]+e[1]*e[1]+e[2]*e[2],r=e[4]*e[4]+e[5]*e[5]+e[6]*e[6],s=e[8]*e[8]+e[9]*e[9]+e[10]*e[10];return Math.sqrt(Math.max(n,r,s))}makeTranslation(e,n,r){return e.isVector3?this.set(1,0,0,e.x,0,1,0,e.y,0,0,1,e.z,0,0,0,1):this.set(1,0,0,e,0,1,0,n,0,0,1,r,0,0,0,1),this}makeRotationX(e){let n=Math.cos(e),r=Math.sin(e);return this.set(1,0,0,0,0,n,-r,0,0,r,n,0,0,0,0,1),this}makeRotationY(e){let n=Math.cos(e),r=Math.sin(e);return this.set(n,0,r,0,0,1,0,0,-r,0,n,0,0,0,0,1),this}makeRotationZ(e){let n=Math.cos(e),r=Math.sin(e);return this.set(n,-r,0,0,r,n,0,0,0,0,1,0,0,0,0,1),this}makeRotationAxis(e,n){let r=Math.cos(n),s=Math.sin(n),o=1-r,a=e.x,l=e.y,c=e.z,u=o*a,h=o*l;return this.set(u*a+r,u*l-s*c,u*c+s*l,0,u*l+s*c,h*l+r,h*c-s*a,0,u*c-s*l,h*c+s*a,o*c*c+r,0,0,0,0,1),this}makeScale(e,n,r){return this.set(e,0,0,0,0,n,0,0,0,0,r,0,0,0,0,1),this}makeShear(e,n,r,s,o,a){return this.set(1,r,o,0,e,1,a,0,n,s,1,0,0,0,0,1),this}compose(e,n,r){let s=this.elements,o=n._x,a=n._y,l=n._z,c=n._w,u=o+o,h=a+a,d=l+l,f=o*u,p=o*h,g=o*d,v=a*h,_=a*d,m=l*d,w=c*u,T=c*h,y=c*d,b=r.x,S=r.y,A=r.z;return s[0]=(1-(v+m))*b,s[1]=(p+y)*b,s[2]=(g-T)*b,s[3]=0,s[4]=(p-y)*S,s[5]=(1-(f+m))*S,s[6]=(_+w)*S,s[7]=0,s[8]=(g+T)*A,s[9]=(_-w)*A,s[10]=(1-(f+v))*A,s[11]=0,s[12]=e.x,s[13]=e.y,s[14]=e.z,s[15]=1,this}decompose(e,n,r){let s=this.elements;e.x=s[12],e.y=s[13],e.z=s[14];let o=this.determinantAffine();if(o===0)return r.set(1,1,1),n.identity(),this;let a=as.set(s[0],s[1],s[2]).length(),l=as.set(s[4],s[5],s[6]).length(),c=as.set(s[8],s[9],s[10]).length();o<0&&(a=-a),Wn.copy(this);let u=1/a,h=1/l,d=1/c;return Wn.elements[0]*=u,Wn.elements[1]*=u,Wn.elements[2]*=u,Wn.elements[4]*=h,Wn.elements[5]*=h,Wn.elements[6]*=h,Wn.elements[8]*=d,Wn.elements[9]*=d,Wn.elements[10]*=d,n.setFromRotationMatrix(Wn),r.x=a,r.y=l,r.z=c,this}makePerspective(e,n,r,s,o,a,l=jn,c=!1){let u=this.elements,h=2*o/(n-e),d=2*o/(r-s),f=(n+e)/(n-e),p=(r+s)/(r-s),g,v;if(c)g=o/(a-o),v=a*o/(a-o);else if(l===jn)g=-(a+o)/(a-o),v=-2*a*o/(a-o);else if(l===ws)g=-a/(a-o),v=-a*o/(a-o);else throw new Error("THREE.Matrix4.makePerspective(): Invalid coordinate system: "+l);return u[0]=h,u[4]=0,u[8]=f,u[12]=0,u[1]=0,u[5]=d,u[9]=p,u[13]=0,u[2]=0,u[6]=0,u[10]=g,u[14]=v,u[3]=0,u[7]=0,u[11]=-1,u[15]=0,this}makeOrthographic(e,n,r,s,o,a,l=jn,c=!1){let u=this.elements,h=2/(n-e),d=2/(r-s),f=-(n+e)/(n-e),p=-(r+s)/(r-s),g,v;if(c)g=1/(a-o),v=a/(a-o);else if(l===jn)g=-2/(a-o),v=-(a+o)/(a-o);else if(l===ws)g=-1/(a-o),v=-o/(a-o);else throw new Error("THREE.Matrix4.makeOrthographic(): Invalid coordinate system: "+l);return u[0]=h,u[4]=0,u[8]=0,u[12]=f,u[1]=0,u[5]=d,u[9]=0,u[13]=p,u[2]=0,u[6]=0,u[10]=g,u[14]=v,u[3]=0,u[7]=0,u[11]=0,u[15]=1,this}equals(e){let n=this.elements,r=e.elements;for(let s=0;s<16;s++)if(n[s]!==r[s])return!1;return!0}fromArray(e,n=0){for(let r=0;r<16;r++)this.elements[r]=e[r+n];return this}toArray(e=[],n=0){let r=this.elements;return e[n]=r[0],e[n+1]=r[1],e[n+2]=r[2],e[n+3]=r[3],e[n+4]=r[4],e[n+5]=r[5],e[n+6]=r[6],e[n+7]=r[7],e[n+8]=r[8],e[n+9]=r[9],e[n+10]=r[10],e[n+11]=r[11],e[n+12]=r[12],e[n+13]=r[13],e[n+14]=r[14],e[n+15]=r[15],e}};uc.prototype.isMatrix4=!0;var st=uc,as=new F,Wn=new st,CS=new F(0,0,0),RS=new F(1,1,1),Xi=new F,el=new F,Sn=new F,_m=new st,vm=new Ut,Ci=class i{constructor(e=0,n=0,r=0,s=i.DEFAULT_ORDER){this.isEuler=!0,this._x=e,this._y=n,this._z=r,this._order=s}get x(){return this._x}set x(e){this._x=e,this._onChangeCallback()}get y(){return this._y}set y(e){this._y=e,this._onChangeCallback()}get z(){return this._z}set z(e){this._z=e,this._onChangeCallback()}get order(){return this._order}set order(e){this._order=e,this._onChangeCallback()}set(e,n,r,s=this._order){return this._x=e,this._y=n,this._z=r,this._order=s,this._onChangeCallback(),this}clone(){return new this.constructor(this._x,this._y,this._z,this._order)}copy(e){return this._x=e._x,this._y=e._y,this._z=e._z,this._order=e._order,this._onChangeCallback(),this}setFromRotationMatrix(e,n=this._order,r=!0){let s=e.elements,o=s[0],a=s[4],l=s[8],c=s[1],u=s[5],h=s[9],d=s[2],f=s[6],p=s[10];switch(n){case"XYZ":this._y=Math.asin(je(l,-1,1)),Math.abs(l)<.9999999?(this._x=Math.atan2(-h,p),this._z=Math.atan2(-a,o)):(this._x=Math.atan2(f,u),this._z=0);break;case"YXZ":this._x=Math.asin(-je(h,-1,1)),Math.abs(h)<.9999999?(this._y=Math.atan2(l,p),this._z=Math.atan2(c,u)):(this._y=Math.atan2(-d,o),this._z=0);break;case"ZXY":this._x=Math.asin(je(f,-1,1)),Math.abs(f)<.9999999?(this._y=Math.atan2(-d,p),this._z=Math.atan2(-a,u)):(this._y=0,this._z=Math.atan2(c,o));break;case"ZYX":this._y=Math.asin(-je(d,-1,1)),Math.abs(d)<.9999999?(this._x=Math.atan2(f,p),this._z=Math.atan2(c,o)):(this._x=0,this._z=Math.atan2(-a,u));break;case"YZX":this._z=Math.asin(je(c,-1,1)),Math.abs(c)<.9999999?(this._x=Math.atan2(-h,u),this._y=Math.atan2(-d,o)):(this._x=0,this._y=Math.atan2(l,p));break;case"XZY":this._z=Math.asin(-je(a,-1,1)),Math.abs(a)<.9999999?(this._x=Math.atan2(f,u),this._y=Math.atan2(l,o)):(this._x=Math.atan2(-h,p),this._y=0);break;default:Ue("Euler: .setFromRotationMatrix() encountered an unknown order: "+n)}return this._order=n,r===!0&&this._onChangeCallback(),this}setFromQuaternion(e,n,r){return _m.makeRotationFromQuaternion(e),this.setFromRotationMatrix(_m,n,r)}setFromVector3(e,n=this._order){return this.set(e.x,e.y,e.z,n)}reorder(e){return vm.setFromEuler(this),this.setFromQuaternion(vm,e)}equals(e){return e._x===this._x&&e._y===this._y&&e._z===this._z&&e._order===this._order}fromArray(e){return this._x=e[0],this._y=e[1],this._z=e[2],e[3]!==void 0&&(this._order=e[3]),this._onChangeCallback(),this}toArray(e=[],n=0){return e[n]=this._x,e[n+1]=this._y,e[n+2]=this._z,e[n+3]=this._order,e}_onChange(e){return this._onChangeCallback=e,this}_onChangeCallback(){}*[Symbol.iterator](){yield this._x,yield this._y,yield this._z,yield this._order}};Ci.DEFAULT_ORDER="XYZ";var Cs=class{constructor(){this.mask=1}set(e){this.mask=(1<<e|0)>>>0}enable(e){this.mask|=1<<e|0}enableAll(){this.mask=-1}toggle(e){this.mask^=1<<e|0}disable(e){this.mask&=~(1<<e|0)}disableAll(){this.mask=0}test(e){return(this.mask&e.mask)!==0}isEnabled(e){return(this.mask&(1<<e|0))!==0}},PS=0,ym=new F,ls=new Ut,Si=new st,tl=new F,fo=new F,IS=new F,LS=new Ut,xm=new F(1,0,0),bm=new F(0,1,0),Sm=new F(0,0,1),wm={type:"added"},DS={type:"removed"},cs={type:"childadded",child:null},Vh={type:"childremoved",child:null},Kt=class i extends Yn{constructor(){super(),this.isObject3D=!0,Object.defineProperty(this,"id",{value:PS++}),this.uuid=Vs(),this.name="",this.type="Object3D",this.parent=null,this.children=[],this.up=i.DEFAULT_UP.clone();let e=new F,n=new Ci,r=new Ut,s=new F(1,1,1);function o(){r.setFromEuler(n,!1)}function a(){n.setFromQuaternion(r,void 0,!1)}n._onChange(o),r._onChange(a),Object.defineProperties(this,{position:{configurable:!0,enumerable:!0,value:e},rotation:{configurable:!0,enumerable:!0,value:n},quaternion:{configurable:!0,enumerable:!0,value:r},scale:{configurable:!0,enumerable:!0,value:s},modelViewMatrix:{value:new st},normalMatrix:{value:new He}}),this.matrix=new st,this.matrixWorld=new st,this.matrixAutoUpdate=i.DEFAULT_MATRIX_AUTO_UPDATE,this.matrixWorldAutoUpdate=i.DEFAULT_MATRIX_WORLD_AUTO_UPDATE,this.matrixWorldNeedsUpdate=!1,this.layers=new Cs,this.visible=!0,this.castShadow=!1,this.receiveShadow=!1,this.frustumCulled=!0,this.renderOrder=0,this.animations=[],this.customDepthMaterial=void 0,this.customDistanceMaterial=void 0,this.static=!1,this.userData={},this.pivot=null}onBeforeShadow(){}onAfterShadow(){}onBeforeRender(){}onAfterRender(){}applyMatrix4(e){this.matrixAutoUpdate&&this.updateMatrix(),this.matrix.premultiply(e),this.matrix.decompose(this.position,this.quaternion,this.scale)}applyQuaternion(e){return this.quaternion.premultiply(e),this}setRotationFromAxisAngle(e,n){this.quaternion.setFromAxisAngle(e,n)}setRotationFromEuler(e){this.quaternion.setFromEuler(e,!0)}setRotationFromMatrix(e){this.quaternion.setFromRotationMatrix(e)}setRotationFromQuaternion(e){this.quaternion.copy(e)}rotateOnAxis(e,n){return ls.setFromAxisAngle(e,n),this.quaternion.multiply(ls),this}rotateOnWorldAxis(e,n){return ls.setFromAxisAngle(e,n),this.quaternion.premultiply(ls),this}rotateX(e){return this.rotateOnAxis(xm,e)}rotateY(e){return this.rotateOnAxis(bm,e)}rotateZ(e){return this.rotateOnAxis(Sm,e)}translateOnAxis(e,n){return ym.copy(e).applyQuaternion(this.quaternion),this.position.add(ym.multiplyScalar(n)),this}translateX(e){return this.translateOnAxis(xm,e)}translateY(e){return this.translateOnAxis(bm,e)}translateZ(e){return this.translateOnAxis(Sm,e)}localToWorld(e){return this.updateWorldMatrix(!0,!1),e.applyMatrix4(this.matrixWorld)}worldToLocal(e){return this.updateWorldMatrix(!0,!1),e.applyMatrix4(Si.copy(this.matrixWorld).invert())}lookAt(e,n,r){e.isVector3?tl.copy(e):tl.set(e,n,r);let s=this.parent;this.updateWorldMatrix(!0,!1),fo.setFromMatrixPosition(this.matrixWorld),this.isCamera||this.isLight?Si.lookAt(fo,tl,this.up):Si.lookAt(tl,fo,this.up),this.quaternion.setFromRotationMatrix(Si),s&&(Si.extractRotation(s.matrixWorld),ls.setFromRotationMatrix(Si),this.quaternion.premultiply(ls.invert()))}add(e){if(arguments.length>1){for(let n=0;n<arguments.length;n++)this.add(arguments[n]);return this}return e===this?(ze("Object3D.add: object can't be added as a child of itself.",e),this):(e&&e.isObject3D?(e.removeFromParent(),e.parent=this,this.children.push(e),e.dispatchEvent(wm),cs.child=e,this.dispatchEvent(cs),cs.child=null):ze("Object3D.add: object not an instance of THREE.Object3D.",e),this)}remove(e){if(arguments.length>1){for(let r=0;r<arguments.length;r++)this.remove(arguments[r]);return this}let n=this.children.indexOf(e);return n!==-1&&(e.parent=null,this.children.splice(n,1),e.dispatchEvent(DS),Vh.child=e,this.dispatchEvent(Vh),Vh.child=null),this}removeFromParent(){let e=this.parent;return e!==null&&e.remove(this),this}clear(){return this.remove(...this.children)}attach(e){return this.updateWorldMatrix(!0,!1),Si.copy(this.matrixWorld).invert(),e.parent!==null&&(e.parent.updateWorldMatrix(!0,!1),Si.multiply(e.parent.matrixWorld)),e.applyMatrix4(Si),e.removeFromParent(),e.parent=this,this.children.push(e),e.updateWorldMatrix(!1,!0),e.dispatchEvent(wm),cs.child=e,this.dispatchEvent(cs),cs.child=null,this}getObjectById(e){return this.getObjectByProperty("id",e)}getObjectByName(e){return this.getObjectByProperty("name",e)}getObjectByProperty(e,n){if(this[e]===n)return this;for(let r=0,s=this.children.length;r<s;r++){let a=this.children[r].getObjectByProperty(e,n);if(a!==void 0)return a}}getObjectsByProperty(e,n,r=[]){this[e]===n&&r.push(this);let s=this.children;for(let o=0,a=s.length;o<a;o++)s[o].getObjectsByProperty(e,n,r);return r}getWorldPosition(e){return this.updateWorldMatrix(!0,!1),e.setFromMatrixPosition(this.matrixWorld)}getWorldQuaternion(e){return this.updateWorldMatrix(!0,!1),this.matrixWorld.decompose(fo,e,IS),e}getWorldScale(e){return this.updateWorldMatrix(!0,!1),this.matrixWorld.decompose(fo,LS,e),e}getWorldDirection(e){this.updateWorldMatrix(!0,!1);let n=this.matrixWorld.elements;return e.set(n[8],n[9],n[10]).normalize()}raycast(){}intersectsFrustum(){}traverse(e){e(this);let n=this.children;for(let r=0,s=n.length;r<s;r++)n[r].traverse(e)}traverseVisible(e){if(this.visible===!1)return;e(this);let n=this.children;for(let r=0,s=n.length;r<s;r++)n[r].traverseVisible(e)}traverseAncestors(e){let n=this.parent;n!==null&&(e(n),n.traverseAncestors(e))}updateMatrix(){this.matrix.compose(this.position,this.quaternion,this.scale);let e=this.pivot;if(e!==null){let n=e.x,r=e.y,s=e.z,o=this.matrix.elements;o[12]+=n-o[0]*n-o[4]*r-o[8]*s,o[13]+=r-o[1]*n-o[5]*r-o[9]*s,o[14]+=s-o[2]*n-o[6]*r-o[10]*s}this.matrixWorldNeedsUpdate=!0}updateMatrixWorld(e){this.matrixAutoUpdate&&this.updateMatrix(),(this.matrixWorldNeedsUpdate||e)&&(this.matrixWorldAutoUpdate===!0&&(this.parent===null?this.matrixWorld.copy(this.matrix):this.matrixWorld.multiplyMatrices(this.parent.matrixWorld,this.matrix)),this.matrixWorldNeedsUpdate=!1,e=!0);let n=this.children;for(let r=0,s=n.length;r<s;r++)n[r].updateMatrixWorld(e)}updateWorldMatrix(e,n,r=!1){let s=this.parent;if(e===!0&&s!==null&&s.updateWorldMatrix(!0,!1),this.matrixAutoUpdate&&this.updateMatrix(),(this.matrixWorldNeedsUpdate||r)&&(this.matrixWorldAutoUpdate===!0&&(this.parent===null?this.matrixWorld.copy(this.matrix):this.matrixWorld.multiplyMatrices(this.parent.matrixWorld,this.matrix)),this.matrixWorldNeedsUpdate=!1,r=!0),n===!0){let o=this.children;for(let a=0,l=o.length;a<l;a++)o[a].updateWorldMatrix(!1,!0,r)}}toJSON(e){let n=e===void 0||typeof e=="string",r={};n&&(e={geometries:{},materials:{},textures:{},images:{},shapes:{},skeletons:{},animations:{},nodes:{}},r.metadata={version:4.7,type:"Object",generator:"Object3D.toJSON"});let s={};s.uuid=this.uuid,s.type=this.type,s.name=this.name,s.castShadow=this.castShadow,s.receiveShadow=this.receiveShadow,s.visible=this.visible,s.frustumCulled=this.frustumCulled,s.renderOrder=this.renderOrder,s.static=this.static,s.matrixAutoUpdate=this.matrixAutoUpdate,Object.keys(this.userData).length>0&&(s.userData=this.userData),s.layers=this.layers.mask,s.matrix=this.matrix.toArray(),s.up=this.up.toArray(),this.pivot!==null&&(s.pivot=this.pivot.toArray()),this.morphTargetDictionary!==void 0&&(s.morphTargetDictionary=Object.assign({},this.morphTargetDictionary)),this.morphTargetInfluences!==void 0&&(s.morphTargetInfluences=this.morphTargetInfluences.slice()),this.isInstancedMesh&&(s.type="InstancedMesh",s.count=this.count,s.instanceMatrix=this.instanceMatrix.toJSON(),this.instanceColor!==null&&(s.instanceColor=this.instanceColor.toJSON())),this.isBatchedMesh&&(s.type="BatchedMesh",s.perObjectFrustumCulled=this.perObjectFrustumCulled,s.sortObjects=this.sortObjects,s.drawRanges=this._drawRanges,s.reservedRanges=this._reservedRanges,s.geometryInfo=this._geometryInfo.map(l=>({...l,boundingBox:l.boundingBox?l.boundingBox.toJSON():void 0,boundingSphere:l.boundingSphere?l.boundingSphere.toJSON():void 0})),s.instanceInfo=this._instanceInfo.map(l=>({...l})),s.availableInstanceIds=this._availableInstanceIds.slice(),s.availableGeometryIds=this._availableGeometryIds.slice(),s.nextIndexStart=this._nextIndexStart,s.nextVertexStart=this._nextVertexStart,s.geometryCount=this._geometryCount,s.maxInstanceCount=this._maxInstanceCount,s.maxVertexCount=this._maxVertexCount,s.maxIndexCount=this._maxIndexCount,s.geometryInitialized=this._geometryInitialized,s.matricesTexture=this._matricesTexture.toJSON(e),s.indirectTexture=this._indirectTexture.toJSON(e),this._colorsTexture!==null&&(s.colorsTexture=this._colorsTexture.toJSON(e)),this.boundingSphere!==null&&(s.boundingSphere=this.boundingSphere.toJSON()),this.boundingBox!==null&&(s.boundingBox=this.boundingBox.toJSON()));function o(l,c){return l[c.uuid]===void 0&&(l[c.uuid]=c.toJSON(e)),c.uuid}if(this.isScene)this.background&&(this.background.isColor?s.background=this.background.toJSON():this.background.isTexture&&(s.background=this.background.toJSON(e).uuid)),this.environment&&this.environment.isTexture&&this.environment.isRenderTargetTexture!==!0&&(s.environment=this.environment.toJSON(e).uuid);else if(this.isMesh||this.isLine||this.isPoints){s.geometry=o(e.geometries,this.geometry);let l=this.geometry.parameters;if(l!==void 0&&l.shapes!==void 0){let c=l.shapes;if(Array.isArray(c))for(let u=0,h=c.length;u<h;u++){let d=c[u];o(e.shapes,d)}else o(e.shapes,c)}}if(this.isSkinnedMesh&&(s.bindMode=this.bindMode,s.bindMatrix=this.bindMatrix.toArray(),this.skeleton!==void 0&&(o(e.skeletons,this.skeleton),s.skeleton=this.skeleton.uuid)),this.material!==void 0)if(Array.isArray(this.material)){let l=[];for(let c=0,u=this.material.length;c<u;c++)l.push(o(e.materials,this.material[c]));s.material=l}else s.material=o(e.materials,this.material);if(this.children.length>0){s.children=[];for(let l=0;l<this.children.length;l++)s.children.push(this.children[l].toJSON(e).object)}if(this.animations.length>0){s.animations=[];for(let l=0;l<this.animations.length;l++){let c=this.animations[l];s.animations.push(o(e.animations,c))}}if(n){let l=a(e.geometries),c=a(e.materials),u=a(e.textures),h=a(e.images),d=a(e.shapes),f=a(e.skeletons),p=a(e.animations),g=a(e.nodes);l.length>0&&(r.geometries=l),c.length>0&&(r.materials=c),u.length>0&&(r.textures=u),h.length>0&&(r.images=h),d.length>0&&(r.shapes=d),f.length>0&&(r.skeletons=f),p.length>0&&(r.animations=p),g.length>0&&(r.nodes=g)}return r.object=s,r;function a(l){let c=[];for(let u in l){let h=l[u];delete h.metadata,c.push(h)}return c}}clone(e){return new this.constructor().copy(this,e)}copy(e,n=!0){if(this.name=e.name,this.up.copy(e.up),this.position.copy(e.position),this.rotation.order=e.rotation.order,this.quaternion.copy(e.quaternion),this.scale.copy(e.scale),this.pivot=e.pivot!==null?e.pivot.clone():null,this.matrix.copy(e.matrix),this.matrixWorld.copy(e.matrixWorld),this.matrixAutoUpdate=e.matrixAutoUpdate,this.matrixWorldAutoUpdate=e.matrixWorldAutoUpdate,this.matrixWorldNeedsUpdate=e.matrixWorldNeedsUpdate,this.layers.mask=e.layers.mask,this.visible=e.visible,this.castShadow=e.castShadow,this.receiveShadow=e.receiveShadow,this.frustumCulled=e.frustumCulled,this.renderOrder=e.renderOrder,this.static=e.static,this.animations=e.animations.slice(),this.userData=JSON.parse(JSON.stringify(e.userData)),n===!0)for(let r=0;r<e.children.length;r++){let s=e.children[r];this.add(s.clone())}return this}dispose(){this.dispatchEvent({type:"dispose"})}};Kt.DEFAULT_UP=new F(0,1,0);Kt.DEFAULT_MATRIX_AUTO_UPDATE=!0;Kt.DEFAULT_MATRIX_WORLD_AUTO_UPDATE=!0;var hi=class extends Kt{constructor(){super(),this.isGroup=!0,this.type="Group"}},OS={type:"move"},Rs=class{constructor(){this._targetRay=null,this._grip=null,this._hand=null}getHandSpace(){return this._hand===null&&(this._hand=new hi,this._hand.matrixAutoUpdate=!1,this._hand.visible=!1,this._hand.joints={},this._hand.inputState={pinching:!1}),this._hand}getTargetRaySpace(){return this._targetRay===null&&(this._targetRay=new hi,this._targetRay.matrixAutoUpdate=!1,this._targetRay.visible=!1,this._targetRay.hasLinearVelocity=!1,this._targetRay.linearVelocity=new F,this._targetRay.hasAngularVelocity=!1,this._targetRay.angularVelocity=new F),this._targetRay}getGripSpace(){return this._grip===null&&(this._grip=new hi,this._grip.matrixAutoUpdate=!1,this._grip.visible=!1,this._grip.hasLinearVelocity=!1,this._grip.linearVelocity=new F,this._grip.hasAngularVelocity=!1,this._grip.angularVelocity=new F,this._grip.eventsEnabled=!1),this._grip}dispatchEvent(e){return this._targetRay!==null&&this._targetRay.dispatchEvent(e),this._grip!==null&&this._grip.dispatchEvent(e),this._hand!==null&&this._hand.dispatchEvent(e),this}connect(e){if(e&&e.hand){let n=this._hand;if(n)for(let r of e.hand.values())this._getHandJoint(n,r)}return this.dispatchEvent({type:"connected",data:e}),this}disconnect(e){return this.dispatchEvent({type:"disconnected",data:e}),this._targetRay!==null&&(this._targetRay.visible=!1),this._grip!==null&&(this._grip.visible=!1),this._hand!==null&&(this._hand.visible=!1),this}update(e,n,r){let s=null,o=null,a=null,l=this._targetRay,c=this._grip,u=this._hand;if(e&&n.session.visibilityState!=="visible-blurred"){if(u&&e.hand){a=!0;for(let v of e.hand.values()){let _=n.getJointPose(v,r),m=this._getHandJoint(u,v);_!==null&&(m.matrix.fromArray(_.transform.matrix),m.matrix.decompose(m.position,m.rotation,m.scale),m.matrixWorldNeedsUpdate=!0,m.jointRadius=_.radius),m.visible=_!==null}let h=u.joints["index-finger-tip"],d=u.joints["thumb-tip"],f=h.position.distanceTo(d.position),p=.02,g=.005;u.inputState.pinching&&f>p+g?(u.inputState.pinching=!1,this.dispatchEvent({type:"pinchend",handedness:e.handedness,target:this})):!u.inputState.pinching&&f<=p-g&&(u.inputState.pinching=!0,this.dispatchEvent({type:"pinchstart",handedness:e.handedness,target:this}))}else c!==null&&e.gripSpace&&(o=n.getPose(e.gripSpace,r),o!==null&&(c.matrix.fromArray(o.transform.matrix),c.matrix.decompose(c.position,c.rotation,c.scale),c.matrixWorldNeedsUpdate=!0,o.linearVelocity?(c.hasLinearVelocity=!0,c.linearVelocity.copy(o.linearVelocity)):c.hasLinearVelocity=!1,o.angularVelocity?(c.hasAngularVelocity=!0,c.angularVelocity.copy(o.angularVelocity)):c.hasAngularVelocity=!1,c.eventsEnabled&&c.dispatchEvent({type:"gripUpdated",data:e,target:this})));l!==null&&(s=n.getPose(e.targetRaySpace,r),s===null&&o!==null&&(s=o),s!==null&&(l.matrix.fromArray(s.transform.matrix),l.matrix.decompose(l.position,l.rotation,l.scale),l.matrixWorldNeedsUpdate=!0,s.linearVelocity?(l.hasLinearVelocity=!0,l.linearVelocity.copy(s.linearVelocity)):l.hasLinearVelocity=!1,s.angularVelocity?(l.hasAngularVelocity=!0,l.angularVelocity.copy(s.angularVelocity)):l.hasAngularVelocity=!1,this.dispatchEvent(OS)))}return l!==null&&(l.visible=s!==null),c!==null&&(c.visible=o!==null),u!==null&&(u.visible=a!==null),this}_getHandJoint(e,n){if(e.joints[n.jointName]===void 0){let r=new hi;r.matrixAutoUpdate=!1,r.visible=!1,e.joints[n.jointName]=r,e.add(r)}return e.joints[n.jointName]}},Ag={aliceblue:15792383,antiquewhite:16444375,aqua:65535,aquamarine:8388564,azure:15794175,beige:16119260,bisque:16770244,black:0,blanchedalmond:16772045,blue:255,blueviolet:9055202,brown:10824234,burlywood:14596231,cadetblue:6266528,chartreuse:8388352,chocolate:13789470,coral:16744272,cornflowerblue:6591981,cornsilk:16775388,crimson:14423100,cyan:65535,darkblue:139,darkcyan:35723,darkgoldenrod:12092939,darkgray:11119017,darkgreen:25600,darkgrey:11119017,darkkhaki:12433259,darkmagenta:9109643,darkolivegreen:5597999,darkorange:16747520,darkorchid:10040012,darkred:9109504,darksalmon:15308410,darkseagreen:9419919,darkslateblue:4734347,darkslategray:3100495,darkslategrey:3100495,darkturquoise:52945,darkviolet:9699539,deeppink:16716947,deepskyblue:49151,dimgray:6908265,dimgrey:6908265,dodgerblue:2003199,firebrick:11674146,floralwhite:16775920,forestgreen:2263842,fuchsia:16711935,gainsboro:14474460,ghostwhite:16316671,gold:16766720,goldenrod:14329120,gray:8421504,green:32768,greenyellow:11403055,grey:8421504,honeydew:15794160,hotpink:16738740,indianred:13458524,indigo:4915330,ivory:16777200,khaki:15787660,lavender:15132410,lavenderblush:16773365,lawngreen:8190976,lemonchiffon:16775885,lightblue:11393254,lightcoral:15761536,lightcyan:14745599,lightgoldenrodyellow:16448210,lightgray:13882323,lightgreen:9498256,lightgrey:13882323,lightpink:16758465,lightsalmon:16752762,lightseagreen:2142890,lightskyblue:8900346,lightslategray:7833753,lightslategrey:7833753,lightsteelblue:11584734,lightyellow:16777184,lime:65280,limegreen:3329330,linen:16445670,magenta:16711935,maroon:8388608,mediumaquamarine:6737322,mediumblue:205,mediumorchid:12211667,mediumpurple:9662683,mediumseagreen:3978097,mediumslateblue:8087790,mediumspringgreen:64154,mediumturquoise:4772300,mediumvioletred:13047173,midnightblue:1644912,mintcream:16121850,mistyrose:16770273,moccasin:16770229,navajowhite:16768685,navy:128,oldlace:16643558,olive:8421376,olivedrab:7048739,orange:16753920,orangered:16729344,orchid:14315734,palegoldenrod:15657130,palegreen:10025880,paleturquoise:11529966,palevioletred:14381203,papayawhip:16773077,peachpuff:16767673,peru:13468991,pink:16761035,plum:14524637,powderblue:11591910,purple:8388736,rebeccapurple:6697881,red:16711680,rosybrown:12357519,royalblue:4286945,saddlebrown:9127187,salmon:16416882,sandybrown:16032864,seagreen:3050327,seashell:16774638,sienna:10506797,silver:12632256,skyblue:8900331,slateblue:6970061,slategray:7372944,slategrey:7372944,snow:16775930,springgreen:65407,steelblue:4620980,tan:13808780,teal:32896,thistle:14204888,tomato:16737095,turquoise:4251856,violet:15631086,wheat:16113331,white:16777215,whitesmoke:16119285,yellow:16776960,yellowgreen:10145074},qi={h:0,s:0,l:0},nl={h:0,s:0,l:0};function Gh(i,e,n){return n<0&&(n+=1),n>1&&(n-=1),n<1/6?i+(e-i)*6*n:n<1/2?e:n<2/3?i+(e-i)*6*(2/3-n):i}var We=class{constructor(e,n,r){return this.isColor=!0,this.r=1,this.g=1,this.b=1,this.set(e,n,r)}set(e,n,r){if(n===void 0&&r===void 0){let s=e;s&&s.isColor?this.copy(s):typeof s=="number"?this.setHex(s):typeof s=="string"&&this.setStyle(s)}else this.setRGB(e,n,r);return this}setScalar(e){return this.r=e,this.g=e,this.b=e,this}setHex(e,n=on){return e=Math.floor(e),this.r=(e>>16&255)/255,this.g=(e>>8&255)/255,this.b=(e&255)/255,Ke.colorSpaceToWorking(this,n),this}setRGB(e,n,r,s=Ke.workingColorSpace){return this.r=e,this.g=n,this.b=r,Ke.colorSpaceToWorking(this,s),this}setHSL(e,n,r,s=Ke.workingColorSpace){if(e=zf(e,1),n=je(n,0,1),r=je(r,0,1),n===0)this.r=this.g=this.b=r;else{let o=r<=.5?r*(1+n):r+n-r*n,a=2*r-o;this.r=Gh(a,o,e+1/3),this.g=Gh(a,o,e),this.b=Gh(a,o,e-1/3)}return Ke.colorSpaceToWorking(this,s),this}setStyle(e,n=on){function r(o){o!==void 0&&parseFloat(o)<1&&Ue("Color: Alpha component of "+e+" will be ignored.")}let s;if(s=/^(\w+)\(([^\)]*)\)/.exec(e)){let o,a=s[1],l=s[2];switch(a){case"rgb":case"rgba":if(o=/^\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*(?:,\s*(\d*\.?\d+)\s*)?$/.exec(l))return r(o[4]),this.setRGB(Math.min(255,parseInt(o[1],10))/255,Math.min(255,parseInt(o[2],10))/255,Math.min(255,parseInt(o[3],10))/255,n);if(o=/^\s*(\d+)\%\s*,\s*(\d+)\%\s*,\s*(\d+)\%\s*(?:,\s*(\d*\.?\d+)\s*)?$/.exec(l))return r(o[4]),this.setRGB(Math.min(100,parseInt(o[1],10))/100,Math.min(100,parseInt(o[2],10))/100,Math.min(100,parseInt(o[3],10))/100,n);break;case"hsl":case"hsla":if(o=/^\s*(\d*\.?\d+)\s*,\s*(\d*\.?\d+)\%\s*,\s*(\d*\.?\d+)\%\s*(?:,\s*(\d*\.?\d+)\s*)?$/.exec(l))return r(o[4]),this.setHSL(parseFloat(o[1])/360,parseFloat(o[2])/100,parseFloat(o[3])/100,n);break;default:Ue("Color: Unknown color model "+e)}}else if(s=/^\#([A-Fa-f\d]+)$/.exec(e)){let o=s[1],a=o.length;if(a===3)return this.setRGB(parseInt(o.charAt(0),16)/15,parseInt(o.charAt(1),16)/15,parseInt(o.charAt(2),16)/15,n);if(a===6)return this.setHex(parseInt(o,16),n);Ue("Color: Invalid hex color "+e)}else if(e&&e.length>0)return this.setColorName(e,n);return this}setColorName(e,n=on){let r=Ag[e.toLowerCase()];return r!==void 0?this.setHex(r,n):Ue("Color: Unknown color "+e),this}clone(){return new this.constructor(this.r,this.g,this.b)}copy(e){return this.r=e.r,this.g=e.g,this.b=e.b,this}copySRGBToLinear(e){return this.r=Ti(e.r),this.g=Ti(e.g),this.b=Ti(e.b),this}copyLinearToSRGB(e){return this.r=bs(e.r),this.g=bs(e.g),this.b=bs(e.b),this}convertSRGBToLinear(){return this.copySRGBToLinear(this),this}convertLinearToSRGB(){return this.copyLinearToSRGB(this),this}getHex(e=on){return Ke.workingToColorSpace(Yt.copy(this),e),Math.round(je(Yt.r*255,0,255))*65536+Math.round(je(Yt.g*255,0,255))*256+Math.round(je(Yt.b*255,0,255))}getHexString(e=on){return("000000"+this.getHex(e).toString(16)).slice(-6)}getHSL(e,n=Ke.workingColorSpace){Ke.workingToColorSpace(Yt.copy(this),n);let r=Yt.r,s=Yt.g,o=Yt.b,a=Math.max(r,s,o),l=Math.min(r,s,o),c,u,h=(l+a)/2;if(l===a)c=0,u=0;else{let d=a-l;switch(u=h<=.5?d/(a+l):d/(2-a-l),a){case r:c=(s-o)/d+(s<o?6:0);break;case s:c=(o-r)/d+2;break;case o:c=(r-s)/d+4;break}c/=6}return e.h=c,e.s=u,e.l=h,e}getRGB(e,n=Ke.workingColorSpace){return Ke.workingToColorSpace(Yt.copy(this),n),e.r=Yt.r,e.g=Yt.g,e.b=Yt.b,e}getStyle(e=on){Ke.workingToColorSpace(Yt.copy(this),e);let n=Yt.r,r=Yt.g,s=Yt.b;return e!==on?`color(${e} ${n.toFixed(3)} ${r.toFixed(3)} ${s.toFixed(3)})`:`rgb(${Math.round(n*255)},${Math.round(r*255)},${Math.round(s*255)})`}offsetHSL(e,n,r){return this.getHSL(qi),this.setHSL(qi.h+e,qi.s+n,qi.l+r)}add(e){return this.r+=e.r,this.g+=e.g,this.b+=e.b,this}addColors(e,n){return this.r=e.r+n.r,this.g=e.g+n.g,this.b=e.b+n.b,this}addScalar(e){return this.r+=e,this.g+=e,this.b+=e,this}sub(e){return this.r=Math.max(0,this.r-e.r),this.g=Math.max(0,this.g-e.g),this.b=Math.max(0,this.b-e.b),this}multiply(e){return this.r*=e.r,this.g*=e.g,this.b*=e.b,this}multiplyScalar(e){return this.r*=e,this.g*=e,this.b*=e,this}lerp(e,n){return this.r+=(e.r-this.r)*n,this.g+=(e.g-this.g)*n,this.b+=(e.b-this.b)*n,this}lerpColors(e,n,r){return this.r=e.r+(n.r-e.r)*r,this.g=e.g+(n.g-e.g)*r,this.b=e.b+(n.b-e.b)*r,this}lerpHSL(e,n){this.getHSL(qi),e.getHSL(nl);let r=yo(qi.h,nl.h,n),s=yo(qi.s,nl.s,n),o=yo(qi.l,nl.l,n);return this.setHSL(r,s,o),this}setFromVector3(e){return this.r=e.x,this.g=e.y,this.b=e.z,this}applyMatrix3(e){let n=this.r,r=this.g,s=this.b,o=e.elements;return this.r=o[0]*n+o[3]*r+o[6]*s,this.g=o[1]*n+o[4]*r+o[7]*s,this.b=o[2]*n+o[5]*r+o[8]*s,this}equals(e){return e.r===this.r&&e.g===this.g&&e.b===this.b}fromArray(e,n=0){return this.r=e[n],this.g=e[n+1],this.b=e[n+2],this}toArray(e=[],n=0){return e[n]=this.r,e[n+1]=this.g,e[n+2]=this.b,e}fromBufferAttribute(e,n){return this.r=e.getX(n),this.g=e.getY(n),this.b=e.getZ(n),this}toJSON(){return this.getHex()}*[Symbol.iterator](){yield this.r,yield this.g,yield this.b}},Yt=new We;We.NAMES=Ag;var Ao=class extends Kt{constructor(){super(),this.isScene=!0,this.type="Scene",this.background=null,this.environment=null,this.fog=null,this.backgroundBlurriness=0,this.backgroundIntensity=1,this.backgroundRotation=new Ci,this.environmentIntensity=1,this.environmentRotation=new Ci,this.overrideMaterial=null,typeof __THREE_DEVTOOLS__<"u"&&__THREE_DEVTOOLS__.dispatchEvent(new CustomEvent("observe",{detail:this}))}copy(e,n){return super.copy(e,n),e.background!==null&&(this.background=e.background.clone()),e.environment!==null&&(this.environment=e.environment.clone()),e.fog!==null&&(this.fog=e.fog.clone()),this.backgroundBlurriness=e.backgroundBlurriness,this.backgroundIntensity=e.backgroundIntensity,this.backgroundRotation.copy(e.backgroundRotation),this.environmentIntensity=e.environmentIntensity,this.environmentRotation.copy(e.environmentRotation),e.overrideMaterial!==null&&(this.overrideMaterial=e.overrideMaterial.clone()),this.matrixAutoUpdate=e.matrixAutoUpdate,this}toJSON(e){let n=super.toJSON(e);return this.fog!==null&&(n.object.fog=this.fog.toJSON()),n.object.backgroundBlurriness=this.backgroundBlurriness,n.object.backgroundIntensity=this.backgroundIntensity,n.object.backgroundRotation=this.backgroundRotation.toArray(),n.object.environmentIntensity=this.environmentIntensity,n.object.environmentRotation=this.environmentRotation.toArray(),n}},Xn=new F,wi=new F,Hh=new F,Mi=new F,us=new F,hs=new F,Mm=new F,Wh=new F,Xh=new F,qh=new F,jh=new vt,Yh=new vt,$h=new vt,Zi=class i{constructor(e=new F,n=new F,r=new F){this.a=e,this.b=n,this.c=r}static getNormal(e,n,r,s){s.subVectors(r,n),Xn.subVectors(e,n),s.cross(Xn);let o=s.lengthSq();return o>0?s.multiplyScalar(1/Math.sqrt(o)):s.set(0,0,0)}static getBarycoord(e,n,r,s,o){Xn.subVectors(s,n),wi.subVectors(r,n),Hh.subVectors(e,n);let a=Xn.dot(Xn),l=Xn.dot(wi),c=Xn.dot(Hh),u=wi.dot(wi),h=wi.dot(Hh),d=a*u-l*l;if(d===0)return o.set(0,0,0),null;let f=1/d,p=(u*c-l*h)*f,g=(a*h-l*c)*f;return o.set(1-p-g,g,p)}static containsPoint(e,n,r,s){return this.getBarycoord(e,n,r,s,Mi)===null?!1:Mi.x>=0&&Mi.y>=0&&Mi.x+Mi.y<=1}static getInterpolation(e,n,r,s,o,a,l,c){return this.getBarycoord(e,n,r,s,Mi)===null?(c.x=0,c.y=0,"z"in c&&(c.z=0),"w"in c&&(c.w=0),null):(c.setScalar(0),c.addScaledVector(o,Mi.x),c.addScaledVector(a,Mi.y),c.addScaledVector(l,Mi.z),c)}static getInterpolatedAttribute(e,n,r,s,o,a){return jh.setScalar(0),Yh.setScalar(0),$h.setScalar(0),jh.fromBufferAttribute(e,n),Yh.fromBufferAttribute(e,r),$h.fromBufferAttribute(e,s),a.setScalar(0),a.addScaledVector(jh,o.x),a.addScaledVector(Yh,o.y),a.addScaledVector($h,o.z),a}static isFrontFacing(e,n,r,s){return Xn.subVectors(r,n),wi.subVectors(e,n),Xn.cross(wi).dot(s)<0}set(e,n,r){return this.a.copy(e),this.b.copy(n),this.c.copy(r),this}setFromPointsAndIndices(e,n,r,s){return this.a.copy(e[n]),this.b.copy(e[r]),this.c.copy(e[s]),this}setFromAttributeAndIndices(e,n,r,s){return this.a.fromBufferAttribute(e,n),this.b.fromBufferAttribute(e,r),this.c.fromBufferAttribute(e,s),this}clone(){return new this.constructor().copy(this)}copy(e){return this.a.copy(e.a),this.b.copy(e.b),this.c.copy(e.c),this}getArea(){return Xn.subVectors(this.c,this.b),wi.subVectors(this.a,this.b),Xn.cross(wi).length()*.5}getMidpoint(e){return e.addVectors(this.a,this.b).add(this.c).multiplyScalar(1/3)}getNormal(e){return i.getNormal(this.a,this.b,this.c,e)}getPlane(e){return e.setFromCoplanarPoints(this.a,this.b,this.c)}getBarycoord(e,n){return i.getBarycoord(e,this.a,this.b,this.c,n)}getInterpolation(e,n,r,s,o){return i.getInterpolation(e,this.a,this.b,this.c,n,r,s,o)}containsPoint(e){return i.containsPoint(e,this.a,this.b,this.c)}isFrontFacing(e){return i.isFrontFacing(this.a,this.b,this.c,e)}intersectsBox(e){return e.intersectsTriangle(this)}closestPointToPoint(e,n){let r=this.a,s=this.b,o=this.c,a,l;us.subVectors(s,r),hs.subVectors(o,r),Wh.subVectors(e,r);let c=us.dot(Wh),u=hs.dot(Wh);if(c<=0&&u<=0)return n.copy(r);Xh.subVectors(e,s);let h=us.dot(Xh),d=hs.dot(Xh);if(h>=0&&d<=h)return n.copy(s);let f=c*d-h*u;if(f<=0&&c>=0&&h<=0)return a=c/(c-h),n.copy(r).addScaledVector(us,a);qh.subVectors(e,o);let p=us.dot(qh),g=hs.dot(qh);if(g>=0&&p<=g)return n.copy(o);let v=p*u-c*g;if(v<=0&&u>=0&&g<=0)return l=u/(u-g),n.copy(r).addScaledVector(hs,l);let _=h*g-p*d;if(_<=0&&d-h>=0&&p-g>=0)return Mm.subVectors(o,s),l=(d-h)/(d-h+(p-g)),n.copy(s).addScaledVector(Mm,l);let m=1/(_+v+f);return a=v*m,l=f*m,n.copy(r).addScaledVector(us,a).addScaledVector(hs,l)}equals(e){return e.a.equals(this.a)&&e.b.equals(this.b)&&e.c.equals(this.c)}},In=class{constructor(e=new F(1/0,1/0,1/0),n=new F(-1/0,-1/0,-1/0)){this.isBox3=!0,this.min=e,this.max=n}set(e,n){return this.min.copy(e),this.max.copy(n),this}setFromArray(e){this.makeEmpty();for(let n=0,r=e.length;n<r;n+=3)this.expandByPoint(qn.fromArray(e,n));return this}setFromBufferAttribute(e){this.makeEmpty();for(let n=0,r=e.count;n<r;n++)this.expandByPoint(qn.fromBufferAttribute(e,n));return this}setFromPoints(e){this.makeEmpty();for(let n=0,r=e.length;n<r;n++)this.expandByPoint(e[n]);return this}setFromCenterAndSize(e,n){let r=qn.copy(n).multiplyScalar(.5);return this.min.copy(e).sub(r),this.max.copy(e).add(r),this}setFromObject(e,n=!1){return this.makeEmpty(),this.expandByObject(e,n)}clone(){return new this.constructor().copy(this)}copy(e){return this.min.copy(e.min),this.max.copy(e.max),this}makeEmpty(){return this.min.x=this.min.y=this.min.z=1/0,this.max.x=this.max.y=this.max.z=-1/0,this}isEmpty(){return this.max.x<this.min.x||this.max.y<this.min.y||this.max.z<this.min.z}getCenter(e){return this.isEmpty()?e.set(0,0,0):e.addVectors(this.min,this.max).multiplyScalar(.5)}getSize(e){return this.isEmpty()?e.set(0,0,0):e.subVectors(this.max,this.min)}expandByPoint(e){return this.min.min(e),this.max.max(e),this}expandByVector(e){return this.min.sub(e),this.max.add(e),this}expandByScalar(e){return this.min.addScalar(-e),this.max.addScalar(e),this}expandByObject(e,n=!1){e.updateWorldMatrix(!1,!1);let r=e.geometry;if(r!==void 0){let o=r.getAttribute("position");if(n===!0&&o!==void 0&&e.isInstancedMesh!==!0)for(let a=0,l=o.count;a<l;a++)e.isMesh===!0?e.getVertexPosition(a,qn):qn.fromBufferAttribute(o,a),qn.applyMatrix4(e.matrixWorld),this.expandByPoint(qn);else e.boundingBox!==void 0?(e.boundingBox===null&&e.computeBoundingBox(),il.copy(e.boundingBox)):(r.boundingBox===null&&r.computeBoundingBox(),il.copy(r.boundingBox)),il.applyMatrix4(e.matrixWorld),this.union(il)}let s=e.children;for(let o=0,a=s.length;o<a;o++)this.expandByObject(s[o],n);return this}containsPoint(e){return e.x>=this.min.x&&e.x<=this.max.x&&e.y>=this.min.y&&e.y<=this.max.y&&e.z>=this.min.z&&e.z<=this.max.z}containsBox(e){return this.min.x<=e.min.x&&e.max.x<=this.max.x&&this.min.y<=e.min.y&&e.max.y<=this.max.y&&this.min.z<=e.min.z&&e.max.z<=this.max.z}getParameter(e,n){return n.set((e.x-this.min.x)/(this.max.x-this.min.x),(e.y-this.min.y)/(this.max.y-this.min.y),(e.z-this.min.z)/(this.max.z-this.min.z))}intersectsBox(e){return e.max.x>=this.min.x&&e.min.x<=this.max.x&&e.max.y>=this.min.y&&e.min.y<=this.max.y&&e.max.z>=this.min.z&&e.min.z<=this.max.z}intersectsSphere(e){return this.clampPoint(e.center,qn),qn.distanceToSquared(e.center)<=e.radius*e.radius}intersectsPlane(e){let n,r;return e.normal.x>0?(n=e.normal.x*this.min.x,r=e.normal.x*this.max.x):(n=e.normal.x*this.max.x,r=e.normal.x*this.min.x),e.normal.y>0?(n+=e.normal.y*this.min.y,r+=e.normal.y*this.max.y):(n+=e.normal.y*this.max.y,r+=e.normal.y*this.min.y),e.normal.z>0?(n+=e.normal.z*this.min.z,r+=e.normal.z*this.max.z):(n+=e.normal.z*this.max.z,r+=e.normal.z*this.min.z),n<=-e.constant&&r>=-e.constant}intersectsTriangle(e){if(this.isEmpty())return!1;this.getCenter(po),rl.subVectors(this.max,po),fs.subVectors(e.a,po),ds.subVectors(e.b,po),ps.subVectors(e.c,po),ji.subVectors(ds,fs),Yi.subVectors(ps,ds),Er.subVectors(fs,ps);let n=[0,-ji.z,ji.y,0,-Yi.z,Yi.y,0,-Er.z,Er.y,ji.z,0,-ji.x,Yi.z,0,-Yi.x,Er.z,0,-Er.x,-ji.y,ji.x,0,-Yi.y,Yi.x,0,-Er.y,Er.x,0];return!Zh(n,fs,ds,ps,rl)||(n=[1,0,0,0,1,0,0,0,1],!Zh(n,fs,ds,ps,rl))?!1:(sl.crossVectors(ji,Yi),n=[sl.x,sl.y,sl.z],Zh(n,fs,ds,ps,rl))}clampPoint(e,n){return n.copy(e).clamp(this.min,this.max)}distanceToPoint(e){return this.clampPoint(e,qn).distanceTo(e)}getBoundingSphere(e){return this.isEmpty()?e.makeEmpty():(this.getCenter(e.center),e.radius=this.getSize(qn).length()*.5),e}intersect(e){return this.min.max(e.min),this.max.min(e.max),this.isEmpty()&&this.makeEmpty(),this}union(e){return this.min.min(e.min),this.max.max(e.max),this}applyMatrix4(e){return this.isEmpty()?this:(Ei[0].set(this.min.x,this.min.y,this.min.z).applyMatrix4(e),Ei[1].set(this.min.x,this.min.y,this.max.z).applyMatrix4(e),Ei[2].set(this.min.x,this.max.y,this.min.z).applyMatrix4(e),Ei[3].set(this.min.x,this.max.y,this.max.z).applyMatrix4(e),Ei[4].set(this.max.x,this.min.y,this.min.z).applyMatrix4(e),Ei[5].set(this.max.x,this.min.y,this.max.z).applyMatrix4(e),Ei[6].set(this.max.x,this.max.y,this.min.z).applyMatrix4(e),Ei[7].set(this.max.x,this.max.y,this.max.z).applyMatrix4(e),this.setFromPoints(Ei),this)}translate(e){return this.min.add(e),this.max.add(e),this}equals(e){return e.min.equals(this.min)&&e.max.equals(this.max)}toJSON(){return{min:this.min.toArray(),max:this.max.toArray()}}fromJSON(e){return this.min.fromArray(e.min),this.max.fromArray(e.max),this}},Ei=[new F,new F,new F,new F,new F,new F,new F,new F],qn=new F,il=new In,fs=new F,ds=new F,ps=new F,ji=new F,Yi=new F,Er=new F,po=new F,rl=new F,sl=new F,Ar=new F;function Zh(i,e,n,r,s){for(let o=0,a=i.length-3;o<=a;o+=3){Ar.fromArray(i,o);let l=s.x*Math.abs(Ar.x)+s.y*Math.abs(Ar.y)+s.z*Math.abs(Ar.z),c=e.dot(Ar),u=n.dot(Ar),h=r.dot(Ar);if(Math.max(-Math.max(c,u,h),Math.min(c,u,h))>l)return!1}return!0}var Ct=new F,ol=new fe,NS=0,pn=class extends Yn{constructor(e,n,r=!1){if(super(),Array.isArray(e))throw new TypeError("THREE.BufferAttribute: array should be a Typed Array.");this.isBufferAttribute=!0,Object.defineProperty(this,"id",{value:NS++}),this.name="",this.array=e,this.itemSize=n,this.count=e!==void 0?e.length/n:0,this.normalized=r,this.usage=bg,this.updateRanges=[],this.gpuType=Jn,this.version=0}onUploadCallback(){}set needsUpdate(e){e===!0&&this.version++}setUsage(e){return this.usage=e,this}addUpdateRange(e,n){this.updateRanges.push({start:e,count:n})}clearUpdateRanges(){this.updateRanges.length=0}copy(e){return this.name=e.name,this.array=new e.array.constructor(e.array),this.itemSize=e.itemSize,this.count=e.count,this.normalized=e.normalized,this.usage=e.usage,this.gpuType=e.gpuType,this}copyAt(e,n,r){e*=this.itemSize,r*=n.itemSize;for(let s=0,o=this.itemSize;s<o;s++)this.array[e+s]=n.array[r+s];return this}copyArray(e){return this.array.set(e),this}applyMatrix3(e){if(this.itemSize===2)for(let n=0,r=this.count;n<r;n++)ol.fromBufferAttribute(this,n),ol.applyMatrix3(e),this.setXY(n,ol.x,ol.y);else if(this.itemSize===3)for(let n=0,r=this.count;n<r;n++)Ct.fromBufferAttribute(this,n),Ct.applyMatrix3(e),this.setXYZ(n,Ct.x,Ct.y,Ct.z);return this}applyMatrix4(e){for(let n=0,r=this.count;n<r;n++)Ct.fromBufferAttribute(this,n),Ct.applyMatrix4(e),this.setXYZ(n,Ct.x,Ct.y,Ct.z);return this}applyNormalMatrix(e){for(let n=0,r=this.count;n<r;n++)Ct.fromBufferAttribute(this,n),Ct.applyNormalMatrix(e),this.setXYZ(n,Ct.x,Ct.y,Ct.z);return this}transformDirection(e){for(let n=0,r=this.count;n<r;n++)Ct.fromBufferAttribute(this,n),Ct.transformDirection(e),this.setXYZ(n,Ct.x,Ct.y,Ct.z);return this}set(e,n=0){return this.array.set(e,n),this}getComponent(e,n){let r=this.array[e*this.itemSize+n];return this.normalized&&(r=xs(r,this.array)),r}setComponent(e,n,r){return this.normalized&&(r=sn(r,this.array)),this.array[e*this.itemSize+n]=r,this}getX(e){let n=this.array[e*this.itemSize];return this.normalized&&(n=xs(n,this.array)),n}setX(e,n){return this.normalized&&(n=sn(n,this.array)),this.array[e*this.itemSize]=n,this}getY(e){let n=this.array[e*this.itemSize+1];return this.normalized&&(n=xs(n,this.array)),n}setY(e,n){return this.normalized&&(n=sn(n,this.array)),this.array[e*this.itemSize+1]=n,this}getZ(e){let n=this.array[e*this.itemSize+2];return this.normalized&&(n=xs(n,this.array)),n}setZ(e,n){return this.normalized&&(n=sn(n,this.array)),this.array[e*this.itemSize+2]=n,this}getW(e){let n=this.array[e*this.itemSize+3];return this.normalized&&(n=xs(n,this.array)),n}setW(e,n){return this.normalized&&(n=sn(n,this.array)),this.array[e*this.itemSize+3]=n,this}setXY(e,n,r){return e*=this.itemSize,this.normalized&&(n=sn(n,this.array),r=sn(r,this.array)),this.array[e+0]=n,this.array[e+1]=r,this}setXYZ(e,n,r,s){return e*=this.itemSize,this.normalized&&(n=sn(n,this.array),r=sn(r,this.array),s=sn(s,this.array)),this.array[e+0]=n,this.array[e+1]=r,this.array[e+2]=s,this}setXYZW(e,n,r,s,o){return e*=this.itemSize,this.normalized&&(n=sn(n,this.array),r=sn(r,this.array),s=sn(s,this.array),o=sn(o,this.array)),this.array[e+0]=n,this.array[e+1]=r,this.array[e+2]=s,this.array[e+3]=o,this}onUpload(e){return this.onUploadCallback=e,this}clone(){return new this.constructor(this.array,this.itemSize).copy(this)}toJSON(){let e={itemSize:this.itemSize,type:this.array.constructor.name,array:Array.from(this.array),normalized:this.normalized};return e.name=this.name,e.usage=this.usage,e.gpuType=this.gpuType,e}dispose(){this.dispatchEvent({type:"dispose"})}};var To=class extends pn{constructor(e,n,r){super(new Uint16Array(e),n,r)}};var Co=class extends pn{constructor(e,n,r){super(new Uint32Array(e),n,r)}};var _t=class extends pn{constructor(e,n,r){super(new Float32Array(e),n,r)}},US=new In,mo=new F,Kh=new F,Pr=class{constructor(e=new F,n=-1){this.isSphere=!0,this.center=e,this.radius=n}set(e,n){return this.center.copy(e),this.radius=n,this}setFromPoints(e,n){let r=this.center;n!==void 0?r.copy(n):US.setFromPoints(e).getCenter(r);let s=0;for(let o=0,a=e.length;o<a;o++)s=Math.max(s,r.distanceToSquared(e[o]));return this.radius=Math.sqrt(s),this}copy(e){return this.center.copy(e.center),this.radius=e.radius,this}isEmpty(){return this.radius<0}makeEmpty(){return this.center.set(0,0,0),this.radius=-1,this}containsPoint(e){return e.distanceToSquared(this.center)<=this.radius*this.radius}distanceToPoint(e){return e.distanceTo(this.center)-this.radius}intersectsSphere(e){let n=this.radius+e.radius;return e.center.distanceToSquared(this.center)<=n*n}intersectsBox(e){return e.intersectsSphere(this)}intersectsPlane(e){return Math.abs(e.distanceToPoint(this.center))<=this.radius}clampPoint(e,n){let r=this.center.distanceToSquared(e);return n.copy(e),r>this.radius*this.radius&&(n.sub(this.center).normalize(),n.multiplyScalar(this.radius).add(this.center)),n}getBoundingBox(e){return this.isEmpty()?(e.makeEmpty(),e):(e.set(this.center,this.center),e.expandByScalar(this.radius),e)}applyMatrix4(e){return this.center.applyMatrix4(e),this.radius=this.radius*e.getMaxScaleOnAxis(),this}translate(e){return this.center.add(e),this}expandByPoint(e){if(this.isEmpty())return this.center.copy(e),this.radius=0,this;mo.subVectors(e,this.center);let n=mo.lengthSq();if(n>this.radius*this.radius){let r=Math.sqrt(n),s=(r-this.radius)*.5;this.center.addScaledVector(mo,s/r),this.radius+=s}return this}union(e){return e.isEmpty()?this:this.isEmpty()?(this.copy(e),this):(this.center.equals(e.center)===!0?this.radius=Math.max(this.radius,e.radius):(Kh.subVectors(e.center,this.center).setLength(e.radius),this.expandByPoint(mo.copy(e.center).add(Kh)),this.expandByPoint(mo.copy(e.center).sub(Kh))),this)}equals(e){return e.center.equals(this.center)&&e.radius===this.radius}clone(){return new this.constructor().copy(this)}toJSON(){return{radius:this.radius,center:this.center.toArray()}}fromJSON(e){return this.radius=e.radius,this.center.fromArray(e.center),this}},FS=0,Pn=new st,Jh=new Kt,ms=new F,wn=new In,go=new In,Ot=new F,Ft=class i extends Yn{constructor(){super(),this.isBufferGeometry=!0,Object.defineProperty(this,"id",{value:FS++}),this.uuid=Vs(),this.name="",this.type="BufferGeometry",this.index=null,this.indirect=null,this.indirectOffset=0,this.attributes={},this.morphAttributes={},this.morphTargetsRelative=!1,this.groups=[],this.boundingBox=null,this.boundingSphere=null,this.drawRange={start:0,count:1/0},this.userData={},this._transformed=!1}getIndex(){return this.index}setIndex(e){return Array.isArray(e)?this.index=new(aS(e)?Co:To)(e,1):this.index=e,this}setIndirect(e,n=0){return this.indirect=e,this.indirectOffset=n,this}getIndirect(){return this.indirect}getAttribute(e){return this.attributes[e]}setAttribute(e,n){return this.attributes[e]=n,this}deleteAttribute(e){return delete this.attributes[e],this}hasAttribute(e){return this.attributes[e]!==void 0}addGroup(e,n,r=0){this.groups.push({start:e,count:n,materialIndex:r})}clearGroups(){this.groups=[]}setDrawRange(e,n){this.drawRange.start=e,this.drawRange.count=n}applyMatrix4(e){let n=this.attributes.position;n!==void 0&&(n.applyMatrix4(e),n.needsUpdate=!0);let r=this.attributes.normal;if(r!==void 0){let o=new He().getNormalMatrix(e);r.applyNormalMatrix(o),r.needsUpdate=!0}let s=this.attributes.tangent;return s!==void 0&&(s.transformDirection(e),s.needsUpdate=!0),this.boundingBox!==null&&this.computeBoundingBox(),this.boundingSphere!==null&&this.computeBoundingSphere(),this._transformed=!0,this}applyQuaternion(e){return Pn.makeRotationFromQuaternion(e),this.applyMatrix4(Pn),this}rotateX(e){return Pn.makeRotationX(e),this.applyMatrix4(Pn),this}rotateY(e){return Pn.makeRotationY(e),this.applyMatrix4(Pn),this}rotateZ(e){return Pn.makeRotationZ(e),this.applyMatrix4(Pn),this}translate(e,n,r){return Pn.makeTranslation(e,n,r),this.applyMatrix4(Pn),this}scale(e,n,r){return Pn.makeScale(e,n,r),this.applyMatrix4(Pn),this}lookAt(e){return Jh.lookAt(e),Jh.updateMatrix(),this.applyMatrix4(Jh.matrix),this}center(){return this.computeBoundingBox(),this.boundingBox.getCenter(ms).negate(),this.translate(ms.x,ms.y,ms.z),this}setFromPoints(e){let n=this.getAttribute("position");if(n===void 0){let r=[];for(let s=0,o=e.length;s<o;s++){let a=e[s];r.push(a.x,a.y,a.z||0)}this.setAttribute("position",new _t(r,3))}else{let r=Math.min(e.length,n.count);for(let s=0;s<r;s++){let o=e[s];n.setXYZ(s,o.x,o.y,o.z||0)}e.length>n.count&&Ue("BufferGeometry: Buffer size too small for points data. Use .dispose() and create a new geometry."),n.needsUpdate=!0}return this}computeBoundingBox(){this.boundingBox===null&&(this.boundingBox=new In);let e=this.attributes.position,n=this.morphAttributes.position;if(e&&e.isGLBufferAttribute){ze("BufferGeometry.computeBoundingBox(): GLBufferAttribute requires a manual bounding box.",this),this.boundingBox.set(new F(-1/0,-1/0,-1/0),new F(1/0,1/0,1/0));return}if(e!==void 0){if(this.boundingBox.setFromBufferAttribute(e),n)for(let r=0,s=n.length;r<s;r++){let o=n[r];wn.setFromBufferAttribute(o),this.morphTargetsRelative?(Ot.addVectors(this.boundingBox.min,wn.min),this.boundingBox.expandByPoint(Ot),Ot.addVectors(this.boundingBox.max,wn.max),this.boundingBox.expandByPoint(Ot)):(this.boundingBox.expandByPoint(wn.min),this.boundingBox.expandByPoint(wn.max))}}else this.boundingBox.makeEmpty();(isNaN(this.boundingBox.min.x)||isNaN(this.boundingBox.min.y)||isNaN(this.boundingBox.min.z))&&ze('BufferGeometry.computeBoundingBox(): Computed min/max have NaN values. The "position" attribute is likely to have NaN values.',this)}computeBoundingSphere(){this.boundingSphere===null&&(this.boundingSphere=new Pr);let e=this.attributes.position,n=this.morphAttributes.position;if(e&&e.isGLBufferAttribute){ze("BufferGeometry.computeBoundingSphere(): GLBufferAttribute requires a manual bounding sphere.",this),this.boundingSphere.set(new F,1/0);return}if(e){let r=this.boundingSphere.center;if(wn.setFromBufferAttribute(e),n)for(let o=0,a=n.length;o<a;o++){let l=n[o];go.setFromBufferAttribute(l),this.morphTargetsRelative?(Ot.addVectors(wn.min,go.min),wn.expandByPoint(Ot),Ot.addVectors(wn.max,go.max),wn.expandByPoint(Ot)):(wn.expandByPoint(go.min),wn.expandByPoint(go.max))}wn.getCenter(r);let s=0;for(let o=0,a=e.count;o<a;o++)Ot.fromBufferAttribute(e,o),s=Math.max(s,r.distanceToSquared(Ot));if(n)for(let o=0,a=n.length;o<a;o++){let l=n[o],c=this.morphTargetsRelative;for(let u=0,h=l.count;u<h;u++)Ot.fromBufferAttribute(l,u),c&&(ms.fromBufferAttribute(e,u),Ot.add(ms)),s=Math.max(s,r.distanceToSquared(Ot))}this.boundingSphere.radius=Math.sqrt(s),isNaN(this.boundingSphere.radius)&&ze('BufferGeometry.computeBoundingSphere(): Computed radius is NaN. The "position" attribute is likely to have NaN values.',this)}}computeTangents(){let e=this.index,n=this.attributes;if(e===null||n.position===void 0||n.normal===void 0||n.uv===void 0){ze("BufferGeometry: .computeTangents() failed. Missing required attributes (index, position, normal or uv)");return}let r=n.position,s=n.normal,o=n.uv,a=this.getAttribute("tangent");(a===void 0||a.count!==r.count)&&(a=new pn(new Float32Array(4*r.count),4),this.setAttribute("tangent",a));let l=[],c=[];for(let x=0;x<r.count;x++)l[x]=new F,c[x]=new F;let u=new F,h=new F,d=new F,f=new fe,p=new fe,g=new fe,v=new F,_=new F;function m(x,C,L){u.fromBufferAttribute(r,x),h.fromBufferAttribute(r,C),d.fromBufferAttribute(r,L),f.fromBufferAttribute(o,x),p.fromBufferAttribute(o,C),g.fromBufferAttribute(o,L),h.sub(u),d.sub(u),p.sub(f),g.sub(f);let P=1/(p.x*g.y-g.x*p.y);isFinite(P)&&(v.copy(h).multiplyScalar(g.y).addScaledVector(d,-p.y).multiplyScalar(P),_.copy(d).multiplyScalar(p.x).addScaledVector(h,-g.x).multiplyScalar(P),l[x].add(v),l[C].add(v),l[L].add(v),c[x].add(_),c[C].add(_),c[L].add(_))}let w=this.groups;w.length===0&&(w=[{start:0,count:e.count}]);for(let x=0,C=w.length;x<C;++x){let L=w[x],P=L.start,O=L.count;for(let U=P,M=P+O;U<M;U+=3)m(e.getX(U+0),e.getX(U+1),e.getX(U+2))}let T=new F,y=new F,b=new F,S=new F;function A(x){b.fromBufferAttribute(s,x),S.copy(b);let C=l[x];T.copy(C),T.sub(b.multiplyScalar(b.dot(C))).normalize(),y.crossVectors(S,C);let P=y.dot(c[x])<0?-1:1;a.setXYZW(x,T.x,T.y,T.z,P)}for(let x=0,C=w.length;x<C;++x){let L=w[x],P=L.start,O=L.count;for(let U=P,M=P+O;U<M;U+=3)A(e.getX(U+0)),A(e.getX(U+1)),A(e.getX(U+2))}this._transformed=!0}computeVertexNormals(){let e=this.index,n=this.getAttribute("position");if(n!==void 0){let r=this.getAttribute("normal");if(r===void 0||r.count!==n.count)r=new pn(new Float32Array(n.count*3),3),this.setAttribute("normal",r);else for(let f=0,p=r.count;f<p;f++)r.setXYZ(f,0,0,0);let s=new F,o=new F,a=new F,l=new F,c=new F,u=new F,h=new F,d=new F;if(e)for(let f=0,p=e.count;f<p;f+=3){let g=e.getX(f+0),v=e.getX(f+1),_=e.getX(f+2);s.fromBufferAttribute(n,g),o.fromBufferAttribute(n,v),a.fromBufferAttribute(n,_),h.subVectors(a,o),d.subVectors(s,o),h.cross(d),l.fromBufferAttribute(r,g),c.fromBufferAttribute(r,v),u.fromBufferAttribute(r,_),l.add(h),c.add(h),u.add(h),r.setXYZ(g,l.x,l.y,l.z),r.setXYZ(v,c.x,c.y,c.z),r.setXYZ(_,u.x,u.y,u.z)}else for(let f=0,p=n.count;f<p;f+=3)s.fromBufferAttribute(n,f+0),o.fromBufferAttribute(n,f+1),a.fromBufferAttribute(n,f+2),h.subVectors(a,o),d.subVectors(s,o),h.cross(d),r.setXYZ(f+0,h.x,h.y,h.z),r.setXYZ(f+1,h.x,h.y,h.z),r.setXYZ(f+2,h.x,h.y,h.z);this.normalizeNormals(),r.needsUpdate=!0}}normalizeNormals(){let e=this.attributes.normal;for(let n=0,r=e.count;n<r;n++)Ot.fromBufferAttribute(e,n),Ot.normalize(),e.setXYZ(n,Ot.x,Ot.y,Ot.z)}toNonIndexed(){function e(l,c){let u=l.array,h=l.itemSize,d=l.normalized,f=new u.constructor(c.length*h),p=0,g=0;for(let v=0,_=c.length;v<_;v++){l.isInterleavedBufferAttribute?p=c[v]*l.data.stride+l.offset:p=c[v]*h;for(let m=0;m<h;m++)f[g++]=u[p++]}return new pn(f,h,d)}if(this.index===null)return Ue("BufferGeometry.toNonIndexed(): BufferGeometry is already non-indexed."),this;let n=new i,r=this.index.array,s=this.attributes;for(let l in s){let c=s[l],u=e(c,r);n.setAttribute(l,u)}let o=this.morphAttributes;for(let l in o){let c=[],u=o[l];for(let h=0,d=u.length;h<d;h++){let f=u[h],p=e(f,r);c.push(p)}n.morphAttributes[l]=c}n.morphTargetsRelative=this.morphTargetsRelative;let a=this.groups;for(let l=0,c=a.length;l<c;l++){let u=a[l];n.addGroup(u.start,u.count,u.materialIndex)}return n}toJSON(){let e={metadata:{version:4.7,type:"BufferGeometry",generator:"BufferGeometry.toJSON"}};if(e.uuid=this.uuid,e.type=this.parameters!==void 0&&this._transformed===!0?"BufferGeometry":this.type,e.name=this.name,Object.keys(this.userData).length>0&&(e.userData=this.userData),this.parameters!==void 0&&this._transformed!==!0){let c=this.parameters;for(let u in c)c[u]!==void 0&&(e[u]=c[u]);return e}e.data={attributes:{}};let n=this.index;n!==null&&(e.data.index={type:n.array.constructor.name,array:Array.prototype.slice.call(n.array)});let r=this.attributes;for(let c in r){let u=r[c];e.data.attributes[c]=u.toJSON(e.data)}let s={},o=!1;for(let c in this.morphAttributes){let u=this.morphAttributes[c],h=[];for(let d=0,f=u.length;d<f;d++){let p=u[d];h.push(p.toJSON(e.data))}h.length>0&&(s[c]=h,o=!0)}o&&(e.data.morphAttributes=s,e.data.morphTargetsRelative=this.morphTargetsRelative);let a=this.groups;a.length>0&&(e.data.groups=JSON.parse(JSON.stringify(a)));let l=this.boundingSphere;return l!==null&&(e.data.boundingSphere=l.toJSON()),e}clone(){return new this.constructor().copy(this)}copy(e){this.index=null,this.attributes={},this.morphAttributes={},this.groups=[],this.boundingBox=null,this.boundingSphere=null;let n={};this.name=e.name;let r=e.index;r!==null&&this.setIndex(r.clone());let s=e.attributes;for(let u in s){let h=s[u];this.setAttribute(u,h.clone(n))}let o=e.morphAttributes;for(let u in o){let h=[],d=o[u];for(let f=0,p=d.length;f<p;f++)h.push(d[f].clone(n));this.morphAttributes[u]=h}this.morphTargetsRelative=e.morphTargetsRelative;let a=e.groups;for(let u=0,h=a.length;u<h;u++){let d=a[u];this.addGroup(d.start,d.count,d.materialIndex)}let l=e.boundingBox;l!==null&&(this.boundingBox=l.clone());let c=e.boundingSphere;return c!==null&&(this.boundingSphere=c.clone()),this.drawRange.start=e.drawRange.start,this.drawRange.count=e.drawRange.count,this.userData=e.userData,this._transformed=e._transformed,this}dispose(){this.dispatchEvent({type:"dispose"})}};var Qh=new F,kS=new F,BS=new He,an=class{constructor(e=new F(1,0,0),n=0){this.isPlane=!0,this.normal=e,this.constant=n}set(e,n){return this.normal.copy(e),this.constant=n,this}setComponents(e,n,r,s){return this.normal.set(e,n,r),this.constant=s,this}setFromNormalAndCoplanarPoint(e,n){return this.normal.copy(e),this.constant=-n.dot(this.normal),this}setFromCoplanarPoints(e,n,r){let s=Qh.subVectors(r,n).cross(kS.subVectors(e,n)).normalize();return this.setFromNormalAndCoplanarPoint(s,e),this}copy(e){return this.normal.copy(e.normal),this.constant=e.constant,this}normalize(){let e=1/this.normal.length();return this.normal.multiplyScalar(e),this.constant*=e,this}negate(){return this.constant*=-1,this.normal.negate(),this}distanceToPoint(e){return this.normal.dot(e)+this.constant}distanceToSphere(e){return this.distanceToPoint(e.center)-e.radius}projectPoint(e,n){return n.copy(e).addScaledVector(this.normal,-this.distanceToPoint(e))}intersectLine(e,n,r=!0){let s=e.delta(Qh),o=this.normal.dot(s);if(o===0)return this.distanceToPoint(e.start)===0?n.copy(e.start):null;let a=-(e.start.dot(this.normal)+this.constant)/o;return r===!0&&(a<0||a>1)?null:n.copy(e.start).addScaledVector(s,a)}intersectsLine(e){let n=this.distanceToPoint(e.start),r=this.distanceToPoint(e.end);return n<0&&r>0||r<0&&n>0}intersectsBox(e){return e.intersectsPlane(this)}intersectsSphere(e){return e.intersectsPlane(this)}coplanarPoint(e){return e.copy(this.normal).multiplyScalar(-this.constant)}applyMatrix4(e,n){let r=n||BS.getNormalMatrix(e),s=this.coplanarPoint(Qh).applyMatrix4(e),o=this.normal.applyMatrix3(r).normalize();return this.constant=-s.dot(o),this}translate(e){return this.constant-=e.dot(this.normal),this}equals(e){return e.normal.equals(this.normal)&&e.constant===this.constant}clone(){return new this.constructor().copy(this)}toJSON(){return{normal:this.normal.toArray(),constant:this.constant}}fromJSON(e){return this.normal.fromArray(e.normal),this.constant=e.constant,this}},zS=0,Ri=class extends Yn{constructor(){super(),this.isMaterial=!0,Object.defineProperty(this,"id",{value:zS++}),this.uuid=Vs(),this.name="",this.type="Material",this.blending=ks,this.side=ir,this.vertexColors=!1,this.opacity=1,this.transparent=!1,this.alphaHash=!1,this.blendSrc=bf,this.blendDst=Sf,this.blendEquation=Ur,this.blendSrcAlpha=null,this.blendDstAlpha=null,this.blendEquationAlpha=null,this.blendColor=new We(0,0,0),this.blendAlpha=0,this.depthFunc=Ss,this.depthTest=!0,this.depthWrite=!0,this.stencilWriteMask=255,this.stencilFunc=pg,this.stencilRef=0,this.stencilFuncMask=255,this.stencilFail=Sl,this.stencilZFail=Sl,this.stencilZPass=Sl,this.stencilWrite=!1,this.clippingPlanes=null,this.clipIntersection=!1,this.clipShadows=!1,this.shadowSide=null,this.colorWrite=!0,this.precision=null,this.polygonOffset=!1,this.polygonOffsetFactor=0,this.polygonOffsetUnits=0,this.dithering=!1,this.alphaToCoverage=!1,this.premultipliedAlpha=!1,this.forceSinglePass=!1,this.allowOverride=!0,this.visible=!0,this.toneMapped=!0,this.userData={},this.version=0,this._alphaTest=0}get alphaTest(){return this._alphaTest}set alphaTest(e){this._alphaTest>0!=e>0&&this.version++,this._alphaTest=e}onBeforeRender(){}onBeforeCompile(){}customProgramCacheKey(){return this.onBeforeCompile.toString()}setValues(e){if(e!==void 0)for(let n in e){let r=e[n];if(r===void 0){Ue(`Material: parameter '${n}' has value of undefined.`);continue}let s=this[n];if(s===void 0){Ue(`Material: '${n}' is not a property of THREE.${this.type}.`);continue}s&&s.isColor?s.set(r):s&&s.isVector2&&r&&r.isVector2||s&&s.isEuler&&r&&r.isEuler||s&&s.isVector3&&r&&r.isVector3?s.copy(r):this[n]=r}}toJSON(e){let n=e===void 0||typeof e=="string";n&&(e={textures:{},images:{}});let r={metadata:{version:4.7,type:"Material",generator:"Material.toJSON"}};r.uuid=this.uuid,r.type=this.type,r.blending=this.blending,r.side=this.side,r.shadowSide=this.shadowSide,r.vertexColors=this.vertexColors,r.opacity=this.opacity,r.transparent=this.transparent,r.blendSrc=this.blendSrc,r.blendDst=this.blendDst,r.blendEquation=this.blendEquation,r.blendSrcAlpha=this.blendSrcAlpha,r.blendDstAlpha=this.blendDstAlpha,r.blendEquationAlpha=this.blendEquationAlpha,r.blendColor=this.blendColor.getHex(),r.blendAlpha=this.blendAlpha,r.depthFunc=this.depthFunc,r.depthTest=this.depthTest,r.depthWrite=this.depthWrite,r.colorWrite=this.colorWrite,r.clipIntersection=this.clipIntersection,r.clipShadows=this.clipShadows,r.stencilWriteMask=this.stencilWriteMask,r.stencilFunc=this.stencilFunc,r.stencilRef=this.stencilRef,r.stencilFuncMask=this.stencilFuncMask,r.stencilFail=this.stencilFail,r.stencilZFail=this.stencilZFail,r.stencilZPass=this.stencilZPass,r.stencilWrite=this.stencilWrite,r.polygonOffset=this.polygonOffset,r.polygonOffsetFactor=this.polygonOffsetFactor,r.polygonOffsetUnits=this.polygonOffsetUnits,r.dithering=this.dithering,r.alphaTest=this.alphaTest,r.alphaHash=this.alphaHash,r.alphaToCoverage=this.alphaToCoverage,r.premultipliedAlpha=this.premultipliedAlpha,r.forceSinglePass=this.forceSinglePass,r.allowOverride=this.allowOverride,r.visible=this.visible,r.toneMapped=this.toneMapped,r.name=this.name,this.color&&this.color.isColor&&(r.color=this.color.getHex()),this.roughness!==void 0&&(r.roughness=this.roughness),this.metalness!==void 0&&(r.metalness=this.metalness),this.sheen!==void 0&&(r.sheen=this.sheen),this.sheenColor&&this.sheenColor.isColor&&(r.sheenColor=this.sheenColor.getHex()),this.sheenRoughness!==void 0&&(r.sheenRoughness=this.sheenRoughness),this.emissive&&this.emissive.isColor&&(r.emissive=this.emissive.getHex()),this.emissiveIntensity!==void 0&&(r.emissiveIntensity=this.emissiveIntensity),this.specular&&this.specular.isColor&&(r.specular=this.specular.getHex()),this.specularIntensity!==void 0&&(r.specularIntensity=this.specularIntensity),this.specularColor&&this.specularColor.isColor&&(r.specularColor=this.specularColor.getHex()),this.shininess!==void 0&&(r.shininess=this.shininess),this.clearcoat!==void 0&&(r.clearcoat=this.clearcoat),this.clearcoatRoughness!==void 0&&(r.clearcoatRoughness=this.clearcoatRoughness),this.clearcoatMap&&this.clearcoatMap.isTexture&&(r.clearcoatMap=this.clearcoatMap.toJSON(e).uuid),this.clearcoatRoughnessMap&&this.clearcoatRoughnessMap.isTexture&&(r.clearcoatRoughnessMap=this.clearcoatRoughnessMap.toJSON(e).uuid),this.clearcoatNormalMap&&this.clearcoatNormalMap.isTexture&&(r.clearcoatNormalMap=this.clearcoatNormalMap.toJSON(e).uuid,r.clearcoatNormalScale=this.clearcoatNormalScale.toArray()),this.sheenColorMap&&this.sheenColorMap.isTexture&&(r.sheenColorMap=this.sheenColorMap.toJSON(e).uuid),this.sheenRoughnessMap&&this.sheenRoughnessMap.isTexture&&(r.sheenRoughnessMap=this.sheenRoughnessMap.toJSON(e).uuid),this.dispersion!==void 0&&(r.dispersion=this.dispersion),this.retroreflectivity!==void 0&&(r.retroreflectivity=this.retroreflectivity),this.iridescence!==void 0&&(r.iridescence=this.iridescence),this.iridescenceIOR!==void 0&&(r.iridescenceIOR=this.iridescenceIOR),this.iridescenceThicknessRange!==void 0&&(r.iridescenceThicknessRange=this.iridescenceThicknessRange),this.iridescenceMap&&this.iridescenceMap.isTexture&&(r.iridescenceMap=this.iridescenceMap.toJSON(e).uuid),this.iridescenceThicknessMap&&this.iridescenceThicknessMap.isTexture&&(r.iridescenceThicknessMap=this.iridescenceThicknessMap.toJSON(e).uuid),this.anisotropy!==void 0&&(r.anisotropy=this.anisotropy),this.anisotropyRotation!==void 0&&(r.anisotropyRotation=this.anisotropyRotation),this.anisotropyMap&&this.anisotropyMap.isTexture&&(r.anisotropyMap=this.anisotropyMap.toJSON(e).uuid),this.map&&this.map.isTexture&&(r.map=this.map.toJSON(e).uuid),this.matcap&&this.matcap.isTexture&&(r.matcap=this.matcap.toJSON(e).uuid),this.alphaMap&&this.alphaMap.isTexture&&(r.alphaMap=this.alphaMap.toJSON(e).uuid),this.lightMap&&this.lightMap.isTexture&&(r.lightMap=this.lightMap.toJSON(e).uuid,r.lightMapIntensity=this.lightMapIntensity),this.aoMap&&this.aoMap.isTexture&&(r.aoMap=this.aoMap.toJSON(e).uuid,r.aoMapIntensity=this.aoMapIntensity),this.bumpMap&&this.bumpMap.isTexture&&(r.bumpMap=this.bumpMap.toJSON(e).uuid,r.bumpScale=this.bumpScale),this.normalMap&&this.normalMap.isTexture&&(r.normalMap=this.normalMap.toJSON(e).uuid,r.normalMapType=this.normalMapType,r.normalScale=this.normalScale.toArray()),this.displacementMap&&this.displacementMap.isTexture&&(r.displacementMap=this.displacementMap.toJSON(e).uuid,r.displacementScale=this.displacementScale,r.displacementBias=this.displacementBias),this.roughnessMap&&this.roughnessMap.isTexture&&(r.roughnessMap=this.roughnessMap.toJSON(e).uuid),this.metalnessMap&&this.metalnessMap.isTexture&&(r.metalnessMap=this.metalnessMap.toJSON(e).uuid),this.emissiveMap&&this.emissiveMap.isTexture&&(r.emissiveMap=this.emissiveMap.toJSON(e).uuid),this.specularMap&&this.specularMap.isTexture&&(r.specularMap=this.specularMap.toJSON(e).uuid),this.specularIntensityMap&&this.specularIntensityMap.isTexture&&(r.specularIntensityMap=this.specularIntensityMap.toJSON(e).uuid),this.specularColorMap&&this.specularColorMap.isTexture&&(r.specularColorMap=this.specularColorMap.toJSON(e).uuid),this.envMap&&this.envMap.isTexture&&(r.envMap=this.envMap.toJSON(e).uuid,this.combine!==void 0&&(r.combine=this.combine)),this.envMapRotation!==void 0&&(r.envMapRotation=this.envMapRotation.toArray()),this.envMapIntensity!==void 0&&(r.envMapIntensity=this.envMapIntensity),this.reflectivity!==void 0&&(r.reflectivity=this.reflectivity),this.refractionRatio!==void 0&&(r.refractionRatio=this.refractionRatio),this.gradientMap&&this.gradientMap.isTexture&&(r.gradientMap=this.gradientMap.toJSON(e).uuid),this.transmission!==void 0&&(r.transmission=this.transmission),this.transmissionMap&&this.transmissionMap.isTexture&&(r.transmissionMap=this.transmissionMap.toJSON(e).uuid),this.thickness!==void 0&&(r.thickness=this.thickness),this.thicknessMap&&this.thicknessMap.isTexture&&(r.thicknessMap=this.thicknessMap.toJSON(e).uuid),this.attenuationDistance!==void 0&&(r.attenuationDistance=this.attenuationDistance),this.attenuationColor!==void 0&&(r.attenuationColor=this.attenuationColor.getHex()),this.size!==void 0&&(r.size=this.size),this.sizeAttenuation!==void 0&&(r.sizeAttenuation=this.sizeAttenuation),Array.isArray(this.clippingPlanes)&&this.clippingPlanes.length>0&&(r.clippingPlanes=this.clippingPlanes.map(o=>o.toJSON())),this.rotation!==void 0&&(r.rotation=this.rotation),this.depthPacking!==void 0&&(r.depthPacking=this.depthPacking),this.linewidth!==void 0&&(r.linewidth=this.linewidth),this.linecap!==void 0&&(r.linecap=this.linecap),this.linejoin!==void 0&&(r.linejoin=this.linejoin),this.dashSize!==void 0&&(r.dashSize=this.dashSize),this.gapSize!==void 0&&(r.gapSize=this.gapSize),this.scale!==void 0&&(r.scale=this.scale),this.wireframe!==void 0&&(r.wireframe=this.wireframe),this.wireframeLinewidth!==void 0&&(r.wireframeLinewidth=this.wireframeLinewidth),this.wireframeLinecap!==void 0&&(r.wireframeLinecap=this.wireframeLinecap),this.wireframeLinejoin!==void 0&&(r.wireframeLinejoin=this.wireframeLinejoin),this.flatShading!==void 0&&(r.flatShading=this.flatShading),this.fog!==void 0&&(r.fog=this.fog),Object.keys(this.userData).length>0&&(r.userData=this.userData);function s(o){let a=[];for(let l in o){let c=o[l];delete c.metadata,a.push(c)}return a}if(n){let o=s(e.textures),a=s(e.images);o.length>0&&(r.textures=o),a.length>0&&(r.images=a)}return r}fromJSON(e,n){if(e.uuid!==void 0&&(this.uuid=e.uuid),e.name!==void 0&&(this.name=e.name),e.color!==void 0&&this.color!==void 0&&this.color.setHex(e.color),e.roughness!==void 0&&(this.roughness=e.roughness),e.metalness!==void 0&&(this.metalness=e.metalness),e.sheen!==void 0&&(this.sheen=e.sheen),e.sheenColor!==void 0&&(this.sheenColor=new We().setHex(e.sheenColor)),e.sheenRoughness!==void 0&&(this.sheenRoughness=e.sheenRoughness),e.emissive!==void 0&&this.emissive!==void 0&&this.emissive.setHex(e.emissive),e.specular!==void 0&&this.specular!==void 0&&this.specular.setHex(e.specular),e.specularIntensity!==void 0&&(this.specularIntensity=e.specularIntensity),e.specularColor!==void 0&&this.specularColor!==void 0&&this.specularColor.setHex(e.specularColor),e.shininess!==void 0&&(this.shininess=e.shininess),e.clearcoat!==void 0&&(this.clearcoat=e.clearcoat),e.clearcoatRoughness!==void 0&&(this.clearcoatRoughness=e.clearcoatRoughness),e.dispersion!==void 0&&(this.dispersion=e.dispersion),e.retroreflectivity!==void 0&&(this.retroreflectivity=e.retroreflectivity),e.iridescence!==void 0&&(this.iridescence=e.iridescence),e.iridescenceIOR!==void 0&&(this.iridescenceIOR=e.iridescenceIOR),e.iridescenceThicknessRange!==void 0&&(this.iridescenceThicknessRange=e.iridescenceThicknessRange),e.transmission!==void 0&&(this.transmission=e.transmission),e.thickness!==void 0&&(this.thickness=e.thickness),e.attenuationDistance!==void 0&&(this.attenuationDistance=e.attenuationDistance),e.attenuationColor!==void 0&&this.attenuationColor!==void 0&&this.attenuationColor.setHex(e.attenuationColor),e.anisotropy!==void 0&&(this.anisotropy=e.anisotropy),e.anisotropyRotation!==void 0&&(this.anisotropyRotation=e.anisotropyRotation),e.fog!==void 0&&(this.fog=e.fog),e.flatShading!==void 0&&(this.flatShading=e.flatShading),e.blending!==void 0&&(this.blending=e.blending),e.combine!==void 0&&(this.combine=e.combine),e.side!==void 0&&(this.side=e.side),e.shadowSide!==void 0&&(this.shadowSide=e.shadowSide),e.opacity!==void 0&&(this.opacity=e.opacity),e.transparent!==void 0&&(this.transparent=e.transparent),e.alphaTest!==void 0&&(this.alphaTest=e.alphaTest),e.alphaHash!==void 0&&(this.alphaHash=e.alphaHash),e.depthFunc!==void 0&&(this.depthFunc=e.depthFunc),e.depthTest!==void 0&&(this.depthTest=e.depthTest),e.depthWrite!==void 0&&(this.depthWrite=e.depthWrite),e.colorWrite!==void 0&&(this.colorWrite=e.colorWrite),e.clippingPlanes!==void 0&&(this.clippingPlanes=e.clippingPlanes.map(r=>new an().fromJSON(r))),e.clipIntersection!==void 0&&(this.clipIntersection=e.clipIntersection),e.clipShadows!==void 0&&(this.clipShadows=e.clipShadows),e.depthPacking!==void 0&&(this.depthPacking=e.depthPacking),e.blendSrc!==void 0&&(this.blendSrc=e.blendSrc),e.blendDst!==void 0&&(this.blendDst=e.blendDst),e.blendEquation!==void 0&&(this.blendEquation=e.blendEquation),e.blendSrcAlpha!==void 0&&(this.blendSrcAlpha=e.blendSrcAlpha),e.blendDstAlpha!==void 0&&(this.blendDstAlpha=e.blendDstAlpha),e.blendEquationAlpha!==void 0&&(this.blendEquationAlpha=e.blendEquationAlpha),e.blendColor!==void 0&&this.blendColor!==void 0&&this.blendColor.setHex(e.blendColor),e.blendAlpha!==void 0&&(this.blendAlpha=e.blendAlpha),e.stencilWriteMask!==void 0&&(this.stencilWriteMask=e.stencilWriteMask),e.stencilFunc!==void 0&&(this.stencilFunc=e.stencilFunc),e.stencilRef!==void 0&&(this.stencilRef=e.stencilRef),e.stencilFuncMask!==void 0&&(this.stencilFuncMask=e.stencilFuncMask),e.stencilFail!==void 0&&(this.stencilFail=e.stencilFail),e.stencilZFail!==void 0&&(this.stencilZFail=e.stencilZFail),e.stencilZPass!==void 0&&(this.stencilZPass=e.stencilZPass),e.stencilWrite!==void 0&&(this.stencilWrite=e.stencilWrite),e.wireframe!==void 0&&(this.wireframe=e.wireframe),e.wireframeLinewidth!==void 0&&(this.wireframeLinewidth=e.wireframeLinewidth),e.wireframeLinecap!==void 0&&(this.wireframeLinecap=e.wireframeLinecap),e.wireframeLinejoin!==void 0&&(this.wireframeLinejoin=e.wireframeLinejoin),e.rotation!==void 0&&(this.rotation=e.rotation),e.linewidth!==void 0&&(this.linewidth=e.linewidth),e.linecap!==void 0&&(this.linecap=e.linecap),e.linejoin!==void 0&&(this.linejoin=e.linejoin),e.dashSize!==void 0&&(this.dashSize=e.dashSize),e.gapSize!==void 0&&(this.gapSize=e.gapSize),e.scale!==void 0&&(this.scale=e.scale),e.polygonOffset!==void 0&&(this.polygonOffset=e.polygonOffset),e.polygonOffsetFactor!==void 0&&(this.polygonOffsetFactor=e.polygonOffsetFactor),e.polygonOffsetUnits!==void 0&&(this.polygonOffsetUnits=e.polygonOffsetUnits),e.dithering!==void 0&&(this.dithering=e.dithering),e.alphaToCoverage!==void 0&&(this.alphaToCoverage=e.alphaToCoverage),e.premultipliedAlpha!==void 0&&(this.premultipliedAlpha=e.premultipliedAlpha),e.forceSinglePass!==void 0&&(this.forceSinglePass=e.forceSinglePass),e.allowOverride!==void 0&&(this.allowOverride=e.allowOverride),e.visible!==void 0&&(this.visible=e.visible),e.toneMapped!==void 0&&(this.toneMapped=e.toneMapped),e.userData!==void 0&&(this.userData=e.userData),e.vertexColors!==void 0&&(typeof e.vertexColors=="number"?this.vertexColors=e.vertexColors>0:this.vertexColors=e.vertexColors),e.size!==void 0&&(this.size=e.size),e.sizeAttenuation!==void 0&&(this.sizeAttenuation=e.sizeAttenuation),e.map!==void 0&&(this.map=n[e.map]||null),e.matcap!==void 0&&(this.matcap=n[e.matcap]||null),e.alphaMap!==void 0&&(this.alphaMap=n[e.alphaMap]||null),e.bumpMap!==void 0&&(this.bumpMap=n[e.bumpMap]||null),e.bumpScale!==void 0&&(this.bumpScale=e.bumpScale),e.normalMap!==void 0&&(this.normalMap=n[e.normalMap]||null),e.normalMapType!==void 0&&(this.normalMapType=e.normalMapType),e.normalScale!==void 0){let r=e.normalScale;Array.isArray(r)===!1&&(r=[r,r]),this.normalScale=new fe().fromArray(r)}return e.displacementMap!==void 0&&(this.displacementMap=n[e.displacementMap]||null),e.displacementScale!==void 0&&(this.displacementScale=e.displacementScale),e.displacementBias!==void 0&&(this.displacementBias=e.displacementBias),e.roughnessMap!==void 0&&(this.roughnessMap=n[e.roughnessMap]||null),e.metalnessMap!==void 0&&(this.metalnessMap=n[e.metalnessMap]||null),e.emissiveMap!==void 0&&(this.emissiveMap=n[e.emissiveMap]||null),e.emissiveIntensity!==void 0&&(this.emissiveIntensity=e.emissiveIntensity),e.specularMap!==void 0&&(this.specularMap=n[e.specularMap]||null),e.specularIntensityMap!==void 0&&(this.specularIntensityMap=n[e.specularIntensityMap]||null),e.specularColorMap!==void 0&&(this.specularColorMap=n[e.specularColorMap]||null),e.envMap!==void 0&&(this.envMap=n[e.envMap]||null),e.envMapRotation!==void 0&&this.envMapRotation.fromArray(e.envMapRotation),e.envMapIntensity!==void 0&&(this.envMapIntensity=e.envMapIntensity),e.reflectivity!==void 0&&(this.reflectivity=e.reflectivity),e.refractionRatio!==void 0&&(this.refractionRatio=e.refractionRatio),e.lightMap!==void 0&&(this.lightMap=n[e.lightMap]||null),e.lightMapIntensity!==void 0&&(this.lightMapIntensity=e.lightMapIntensity),e.aoMap!==void 0&&(this.aoMap=n[e.aoMap]||null),e.aoMapIntensity!==void 0&&(this.aoMapIntensity=e.aoMapIntensity),e.gradientMap!==void 0&&(this.gradientMap=n[e.gradientMap]||null),e.clearcoatMap!==void 0&&(this.clearcoatMap=n[e.clearcoatMap]||null),e.clearcoatRoughnessMap!==void 0&&(this.clearcoatRoughnessMap=n[e.clearcoatRoughnessMap]||null),e.clearcoatNormalMap!==void 0&&(this.clearcoatNormalMap=n[e.clearcoatNormalMap]||null),e.clearcoatNormalScale!==void 0&&(this.clearcoatNormalScale=new fe().fromArray(e.clearcoatNormalScale)),e.iridescenceMap!==void 0&&(this.iridescenceMap=n[e.iridescenceMap]||null),e.iridescenceThicknessMap!==void 0&&(this.iridescenceThicknessMap=n[e.iridescenceThicknessMap]||null),e.transmissionMap!==void 0&&(this.transmissionMap=n[e.transmissionMap]||null),e.thicknessMap!==void 0&&(this.thicknessMap=n[e.thicknessMap]||null),e.anisotropyMap!==void 0&&(this.anisotropyMap=n[e.anisotropyMap]||null),e.sheenColorMap!==void 0&&(this.sheenColorMap=n[e.sheenColorMap]||null),e.sheenRoughnessMap!==void 0&&(this.sheenRoughnessMap=n[e.sheenRoughnessMap]||null),this}clone(){return new this.constructor().copy(this)}copy(e){this.name=e.name,this.blending=e.blending,this.side=e.side,this.vertexColors=e.vertexColors,this.opacity=e.opacity,this.transparent=e.transparent,this.blendSrc=e.blendSrc,this.blendDst=e.blendDst,this.blendEquation=e.blendEquation,this.blendSrcAlpha=e.blendSrcAlpha,this.blendDstAlpha=e.blendDstAlpha,this.blendEquationAlpha=e.blendEquationAlpha,this.blendColor.copy(e.blendColor),this.blendAlpha=e.blendAlpha,this.depthFunc=e.depthFunc,this.depthTest=e.depthTest,this.depthWrite=e.depthWrite,this.stencilWriteMask=e.stencilWriteMask,this.stencilFunc=e.stencilFunc,this.stencilRef=e.stencilRef,this.stencilFuncMask=e.stencilFuncMask,this.stencilFail=e.stencilFail,this.stencilZFail=e.stencilZFail,this.stencilZPass=e.stencilZPass,this.stencilWrite=e.stencilWrite;let n=e.clippingPlanes,r=null;if(n!==null){let s=n.length;r=new Array(s);for(let o=0;o!==s;++o)r[o]=n[o].clone()}return this.clippingPlanes=r,this.clipIntersection=e.clipIntersection,this.clipShadows=e.clipShadows,this.shadowSide=e.shadowSide,this.colorWrite=e.colorWrite,this.precision=e.precision,this.polygonOffset=e.polygonOffset,this.polygonOffsetFactor=e.polygonOffsetFactor,this.polygonOffsetUnits=e.polygonOffsetUnits,this.dithering=e.dithering,this.alphaTest=e.alphaTest,this.alphaHash=e.alphaHash,this.alphaToCoverage=e.alphaToCoverage,this.premultipliedAlpha=e.premultipliedAlpha,this.forceSinglePass=e.forceSinglePass,this.allowOverride=e.allowOverride,this.visible=e.visible,this.toneMapped=e.toneMapped,this.userData=JSON.parse(JSON.stringify(e.userData)),this}dispose(){this.dispatchEvent({type:"dispose"})}set needsUpdate(e){e===!0&&this.version++}};var Ai=new F,ef=new F,al=new F,ll=new F,Ki=class{constructor(e=new F,n=new F(0,0,-1)){this.origin=e,this.direction=n}set(e,n){return this.origin.copy(e),this.direction.copy(n),this}copy(e){return this.origin.copy(e.origin),this.direction.copy(e.direction),this}at(e,n){return n.copy(this.origin).addScaledVector(this.direction,e)}lookAt(e){return this.direction.copy(e).sub(this.origin).normalize(),this}recast(e){return this.origin.copy(this.at(e,Ai)),this}closestPointToPoint(e,n){n.subVectors(e,this.origin);let r=n.dot(this.direction);return r<0?n.copy(this.origin):n.copy(this.origin).addScaledVector(this.direction,r)}distanceToPoint(e){return Math.sqrt(this.distanceSqToPoint(e))}distanceSqToPoint(e){let n=Ai.subVectors(e,this.origin).dot(this.direction);return n<0?this.origin.distanceToSquared(e):(Ai.copy(this.origin).addScaledVector(this.direction,n),Ai.distanceToSquared(e))}distanceSqToSegment(e,n,r,s){ef.copy(e).add(n).multiplyScalar(.5),al.copy(n).sub(e).normalize(),ll.copy(this.origin).sub(ef);let o=e.distanceTo(n)*.5,a=-this.direction.dot(al),l=ll.dot(this.direction),c=-ll.dot(al),u=ll.lengthSq(),h=Math.abs(1-a*a),d,f,p,g;if(h>0)if(d=a*c-l,f=a*l-c,g=o*h,d>=0)if(f>=-g)if(f<=g){let v=1/h;d*=v,f*=v,p=d*(d+a*f+2*l)+f*(a*d+f+2*c)+u}else f=o,d=Math.max(0,-(a*f+l)),p=-d*d+f*(f+2*c)+u;else f=-o,d=Math.max(0,-(a*f+l)),p=-d*d+f*(f+2*c)+u;else f<=-g?(d=Math.max(0,-(-a*o+l)),f=d>0?-o:Math.min(Math.max(-o,-c),o),p=-d*d+f*(f+2*c)+u):f<=g?(d=0,f=Math.min(Math.max(-o,-c),o),p=f*(f+2*c)+u):(d=Math.max(0,-(a*o+l)),f=d>0?o:Math.min(Math.max(-o,-c),o),p=-d*d+f*(f+2*c)+u);else f=a>0?-o:o,d=Math.max(0,-(a*f+l)),p=-d*d+f*(f+2*c)+u;return r&&r.copy(this.origin).addScaledVector(this.direction,d),s&&s.copy(ef).addScaledVector(al,f),p}intersectSphere(e,n){if(e.radius<0)return null;Ai.subVectors(e.center,this.origin);let r=Ai.dot(this.direction),s=Ai.dot(Ai)-r*r,o=e.radius*e.radius;if(s>o)return null;let a=Math.sqrt(o-s),l=r-a,c=r+a;return c<0?null:l<0?this.at(c,n):this.at(l,n)}intersectsSphere(e){return e.radius<0?!1:this.distanceSqToPoint(e.center)<=e.radius*e.radius}distanceToPlane(e){let n=e.normal.dot(this.direction);if(n===0)return e.distanceToPoint(this.origin)===0?0:null;let r=-(this.origin.dot(e.normal)+e.constant)/n;return r>=0?r:null}intersectPlane(e,n){let r=this.distanceToPlane(e);return r===null?null:this.at(r,n)}intersectsPlane(e){let n=e.distanceToPoint(this.origin);return n===0||e.normal.dot(this.direction)*n<0}intersectBox(e,n){let r,s,o,a,l,c,u=1/this.direction.x,h=1/this.direction.y,d=1/this.direction.z,f=this.origin;return u>=0?(r=(e.min.x-f.x)*u,s=(e.max.x-f.x)*u):(r=(e.max.x-f.x)*u,s=(e.min.x-f.x)*u),h>=0?(o=(e.min.y-f.y)*h,a=(e.max.y-f.y)*h):(o=(e.max.y-f.y)*h,a=(e.min.y-f.y)*h),r>a||o>s||((o>r||isNaN(r))&&(r=o),(a<s||isNaN(s))&&(s=a),d>=0?(l=(e.min.z-f.z)*d,c=(e.max.z-f.z)*d):(l=(e.max.z-f.z)*d,c=(e.min.z-f.z)*d),r>c||l>s)||((l>r||r!==r)&&(r=l),(c<s||s!==s)&&(s=c),s<0)?null:this.at(r>=0?r:s,n)}intersectsBox(e){return this.intersectBox(e,Ai)!==null}intersectTriangle(e,n,r,s,o){let a=this.origin,l=this.direction,c=l.x,u=l.y,h=l.z,d=e.x-a.x,f=e.y-a.y,p=e.z-a.z,g=n.x-a.x,v=n.y-a.y,_=n.z-a.z,m=r.x-a.x,w=r.y-a.y,T=r.z-a.z,y=Math.abs(c),b=Math.abs(u),S=Math.abs(h),A,x,C,L,P,O,U,M,I,D,k,q;if(y>=b&&y>=S?(C=c,O=d,I=g,q=m,c>=0?(A=u,x=h,L=f,P=p,U=v,M=_,D=w,k=T):(A=h,x=u,L=p,P=f,U=_,M=v,D=T,k=w)):b>=S?(C=u,O=f,I=v,q=w,u>=0?(A=h,x=c,L=p,P=d,U=_,M=g,D=T,k=m):(A=c,x=h,L=d,P=p,U=g,M=_,D=m,k=T)):(C=h,O=p,I=_,q=T,h>=0?(A=c,x=u,L=d,P=f,U=g,M=v,D=m,k=w):(A=u,x=c,L=f,P=d,U=v,M=g,D=w,k=m)),C===0)return null;let W=A/C,j=x/C,B=1/C,Q=L-W*O,re=P-j*O,be=U-W*I,ee=M-j*I,oe=D-W*q,V=k-j*q,Z=oe*ee-V*be,ce=Q*V-re*oe,Ee=be*re-ee*Q;if(s){if(Z<0||ce<0||Ee<0)return null}else if((Z<0||ce<0||Ee<0)&&(Z>0||ce>0||Ee>0))return null;let ue=Z+ce+Ee;if(ue===0)return null;let Be=B*(Z*O+ce*I+Ee*q);return(ue>0?Be<0:Be>0)?null:this.at(Be/ue,o)}applyMatrix4(e){return this.origin.applyMatrix4(e),this.direction.transformDirection(e),this}equals(e){return e.origin.equals(this.origin)&&e.direction.equals(this.direction)}clone(){return new this.constructor().copy(this)}},Ir=class extends Ri{constructor(e){super(),this.isMeshBasicMaterial=!0,this.type="MeshBasicMaterial",this.color=new We(16777215),this.map=null,this.lightMap=null,this.lightMapIntensity=1,this.aoMap=null,this.aoMapIntensity=1,this.specularMap=null,this.alphaMap=null,this.envMap=null,this.envMapRotation=new Ci,this.combine=hc,this.reflectivity=1,this.refractionRatio=.98,this.wireframe=!1,this.wireframeLinewidth=1,this.wireframeLinecap="round",this.wireframeLinejoin="round",this.fog=!0,this.setValues(e)}copy(e){return super.copy(e),this.color.copy(e.color),this.map=e.map,this.lightMap=e.lightMap,this.lightMapIntensity=e.lightMapIntensity,this.aoMap=e.aoMap,this.aoMapIntensity=e.aoMapIntensity,this.specularMap=e.specularMap,this.alphaMap=e.alphaMap,this.envMap=e.envMap,this.envMapRotation.copy(e.envMapRotation),this.combine=e.combine,this.reflectivity=e.reflectivity,this.refractionRatio=e.refractionRatio,this.wireframe=e.wireframe,this.wireframeLinewidth=e.wireframeLinewidth,this.wireframeLinecap=e.wireframeLinecap,this.wireframeLinejoin=e.wireframeLinejoin,this.fog=e.fog,this}},Em=new st,Tr=new Ki,cl=new Pr,Am=new F,ul=new F,hl=new F,fl=new F,tf=new F,dl=new F,Tm=new F,pl=new F,kt=class extends Kt{constructor(e=new Ft,n=new Ir){super(),this.isMesh=!0,this.type="Mesh",this.geometry=e,this.material=n,this.morphTargetDictionary=void 0,this.morphTargetInfluences=void 0,this.count=1,this.updateMorphTargets()}copy(e,n){return super.copy(e,n),e.morphTargetInfluences!==void 0&&(this.morphTargetInfluences=e.morphTargetInfluences.slice()),e.morphTargetDictionary!==void 0&&(this.morphTargetDictionary=Object.assign({},e.morphTargetDictionary)),this.material=Array.isArray(e.material)?e.material.slice():e.material,this.geometry=e.geometry,this}updateMorphTargets(){let n=this.geometry.morphAttributes,r=Object.keys(n);if(r.length>0){let s=n[r[0]];if(s!==void 0){this.morphTargetInfluences=[],this.morphTargetDictionary={};for(let o=0,a=s.length;o<a;o++){let l=s[o].name||String(o);this.morphTargetInfluences.push(0),this.morphTargetDictionary[l]=o}}}}getVertexPosition(e,n){let r=this.geometry,s=r.attributes.position,o=r.morphAttributes.position,a=r.morphTargetsRelative;n.fromBufferAttribute(s,e);let l=this.morphTargetInfluences;if(o&&l){dl.set(0,0,0);for(let c=0,u=o.length;c<u;c++){let h=l[c],d=o[c];h!==0&&(tf.fromBufferAttribute(d,e),a?dl.addScaledVector(tf,h):dl.addScaledVector(tf.sub(n),h))}n.add(dl)}return n}intersectsFrustum(e){return e.intersectsObject(this)}raycast(e,n){let r=this.geometry,s=this.material,o=this.matrixWorld;s!==void 0&&(r.boundingSphere===null&&r.computeBoundingSphere(),cl.copy(r.boundingSphere),cl.applyMatrix4(o),Tr.copy(e.ray).recast(e.near),!(cl.containsPoint(Tr.origin)===!1&&(Tr.intersectSphere(cl,Am)===null||Tr.origin.distanceToSquared(Am)>(e.far-e.near)**2))&&(Em.copy(o).invert(),Tr.copy(e.ray).applyMatrix4(Em),!(r.boundingBox!==null&&Tr.intersectsBox(r.boundingBox)===!1)&&this._computeIntersections(e,n,Tr)))}_computeIntersections(e,n,r){let s,o=this.geometry,a=this.material,l=o.index,c=o.attributes.position,u=o.attributes.uv,h=o.attributes.uv1,d=o.attributes.normal,f=o.groups,p=o.drawRange;if(l!==null)if(Array.isArray(a))for(let g=0,v=f.length;g<v;g++){let _=f[g],m=a[_.materialIndex],w=Math.max(_.start,p.start),T=Math.min(l.count,Math.min(_.start+_.count,p.start+p.count));for(let y=w,b=T;y<b;y+=3){let S=l.getX(y),A=l.getX(y+1),x=l.getX(y+2);s=ml(this,m,e,r,u,h,d,S,A,x),s&&(s.faceIndex=Math.floor(y/3),s.face.materialIndex=_.materialIndex,n.push(s))}}else{let g=Math.max(0,p.start),v=Math.min(l.count,p.start+p.count);for(let _=g,m=v;_<m;_+=3){let w=l.getX(_),T=l.getX(_+1),y=l.getX(_+2);s=ml(this,a,e,r,u,h,d,w,T,y),s&&(s.faceIndex=Math.floor(_/3),n.push(s))}}else if(c!==void 0)if(Array.isArray(a))for(let g=0,v=f.length;g<v;g++){let _=f[g],m=a[_.materialIndex],w=Math.max(_.start,p.start),T=Math.min(c.count,Math.min(_.start+_.count,p.start+p.count));for(let y=w,b=T;y<b;y+=3){let S=y,A=y+1,x=y+2;s=ml(this,m,e,r,u,h,d,S,A,x),s&&(s.faceIndex=Math.floor(y/3),s.face.materialIndex=_.materialIndex,n.push(s))}}else{let g=Math.max(0,p.start),v=Math.min(c.count,p.start+p.count);for(let _=g,m=v;_<m;_+=3){let w=_,T=_+1,y=_+2;s=ml(this,a,e,r,u,h,d,w,T,y),s&&(s.faceIndex=Math.floor(_/3),n.push(s))}}}};function VS(i,e,n,r,s,o,a,l){let c;if(e.side===Gt?c=r.intersectTriangle(a,o,s,!0,l):c=r.intersectTriangle(s,o,a,e.side===ir,l),c===null)return null;pl.copy(l),pl.applyMatrix4(i.matrixWorld);let u=n.ray.origin.distanceTo(pl);return u<n.near||u>n.far?null:{distance:u,point:pl.clone(),object:i}}function ml(i,e,n,r,s,o,a,l,c,u){i.getVertexPosition(l,ul),i.getVertexPosition(c,hl),i.getVertexPosition(u,fl);let h=VS(i,e,n,r,ul,hl,fl,Tm);if(h){let d=new F;Zi.getBarycoord(Tm,ul,hl,fl,d),s&&(h.uv=Zi.getInterpolatedAttribute(s,l,c,u,d,new fe)),o&&(h.uv1=Zi.getInterpolatedAttribute(o,l,c,u,d,new fe)),a&&(h.normal=Zi.getInterpolatedAttribute(a,l,c,u,d,new F),h.normal.dot(r.direction)>0&&h.normal.multiplyScalar(-1));let f={a:l,b:c,c:u,normal:new F,materialIndex:0};Zi.getNormal(ul,hl,fl,f.normal),h.face=f,h.barycoord=d}return h}var Fl=class extends ln{constructor(e=null,n=1,r=1,s,o,a,l,c,u=Nt,h=Nt,d,f){super(null,a,l,c,u,h,s,o,d,f),this.isDataTexture=!0,this.image={data:e,width:n,height:r},this.generateMipmaps=!1,this.flipY=!1,this.unpackAlignment=1}};var Cr=new Pr,GS=new fe(.5,.5),gl=new F,Ps=class{constructor(e=new an,n=new an,r=new an,s=new an,o=new an,a=new an){this.planes=[e,n,r,s,o,a]}set(e,n,r,s,o,a){let l=this.planes;return l[0].copy(e),l[1].copy(n),l[2].copy(r),l[3].copy(s),l[4].copy(o),l[5].copy(a),this}copy(e){let n=this.planes;for(let r=0;r<6;r++)n[r].copy(e.planes[r]);return this}setFromProjectionMatrix(e,n=jn,r=!1){let s=this.planes,o=e.elements,a=o[0],l=o[1],c=o[2],u=o[3],h=o[4],d=o[5],f=o[6],p=o[7],g=o[8],v=o[9],_=o[10],m=o[11],w=o[12],T=o[13],y=o[14],b=o[15];if(s[0].setComponents(u-a,p-h,m-g,b-w).normalize(),s[1].setComponents(u+a,p+h,m+g,b+w).normalize(),s[2].setComponents(u+l,p+d,m+v,b+T).normalize(),s[3].setComponents(u-l,p-d,m-v,b-T).normalize(),r)s[4].setComponents(c,f,_,y).normalize(),s[5].setComponents(u-c,p-f,m-_,b-y).normalize();else if(s[4].setComponents(u-c,p-f,m-_,b-y).normalize(),n===jn)s[5].setComponents(u+c,p+f,m+_,b+y).normalize();else if(n===ws)s[5].setComponents(c,f,_,y).normalize();else throw new Error("THREE.Frustum.setFromProjectionMatrix(): Invalid coordinate system: "+n);return this}intersectsObject(e){if(e.boundingSphere!==void 0)e.boundingSphere===null&&e.computeBoundingSphere(),Cr.copy(e.boundingSphere).applyMatrix4(e.matrixWorld);else{let n=e.geometry;n.boundingSphere===null&&n.computeBoundingSphere(),Cr.copy(n.boundingSphere).applyMatrix4(e.matrixWorld)}return this.intersectsSphere(Cr)}intersectsSprite(e){Cr.center.set(0,0,0);let n=GS.distanceTo(e.center);return Cr.radius=.7071067811865476+n,Cr.applyMatrix4(e.matrixWorld),this.intersectsSphere(Cr)}intersectsSphere(e){let n=this.planes,r=e.center,s=-e.radius;for(let o=0;o<6;o++)if(n[o].distanceToPoint(r)<s)return!1;return!0}intersectsBox(e){let n=this.planes;for(let r=0;r<6;r++){let s=n[r];if(gl.x=s.normal.x>0?e.max.x:e.min.x,gl.y=s.normal.y>0?e.max.y:e.min.y,gl.z=s.normal.z>0?e.max.z:e.min.z,s.distanceToPoint(gl)<0)return!1}return!0}containsPoint(e){let n=this.planes;for(let r=0;r<6;r++)if(n[r].distanceToPoint(e)<0)return!1;return!0}clone(){return new this.constructor().copy(this)}};var Is=class extends Ri{constructor(e){super(),this.isLineBasicMaterial=!0,this.type="LineBasicMaterial",this.color=new We(16777215),this.map=null,this.linewidth=1,this.linecap="round",this.linejoin="round",this.fog=!0,this.setValues(e)}copy(e){return super.copy(e),this.color.copy(e.color),this.map=e.map,this.linewidth=e.linewidth,this.linecap=e.linecap,this.linejoin=e.linejoin,this.fog=e.fog,this}},kl=new F,Bl=new F,Cm=new st,_o=new Ki,_l=new Pr,nf=new F,Rm=new F,Ro=class extends Kt{constructor(e=new Ft,n=new Is){super(),this.isLine=!0,this.type="Line",this.geometry=e,this.material=n,this.morphTargetDictionary=void 0,this.morphTargetInfluences=void 0,this.updateMorphTargets()}copy(e,n){return super.copy(e,n),this.material=Array.isArray(e.material)?e.material.slice():e.material,this.geometry=e.geometry,this}computeLineDistances(){let e=this.geometry;if(e.index===null){let n=e.attributes.position,r=[0];for(let s=1,o=n.count;s<o;s++)kl.fromBufferAttribute(n,s-1),Bl.fromBufferAttribute(n,s),r[s]=r[s-1],r[s]+=kl.distanceTo(Bl);e.setAttribute("lineDistance",new _t(r,1))}else Ue("Line.computeLineDistances(): Computation only possible with non-indexed BufferGeometry.");return this}intersectsFrustum(e){return e.intersectsObject(this)}raycast(e,n){let r=this.geometry,s=this.matrixWorld,o=e.params.Line.threshold,a=r.drawRange;if(r.boundingSphere===null&&r.computeBoundingSphere(),_l.copy(r.boundingSphere),_l.applyMatrix4(s),_l.radius+=o,e.ray.intersectsSphere(_l)===!1)return;Cm.copy(s).invert(),_o.copy(e.ray).applyMatrix4(Cm);let l=o/((this.scale.x+this.scale.y+this.scale.z)/3),c=l*l,u=this.isLineSegments?2:1,h=r.index,f=r.attributes.position;if(h!==null){let p=Math.max(0,a.start),g=Math.min(h.count,a.start+a.count);for(let v=p,_=g-1;v<_;v+=u){let m=h.getX(v),w=h.getX(v+1),T=vl(this,e,_o,c,m,w,v);T&&n.push(T)}if(this.isLineLoop){let v=h.getX(g-1),_=h.getX(p),m=vl(this,e,_o,c,v,_,g-1);m&&n.push(m)}}else{let p=Math.max(0,a.start),g=Math.min(f.count,a.start+a.count);for(let v=p,_=g-1;v<_;v+=u){let m=vl(this,e,_o,c,v,v+1,v);m&&n.push(m)}if(this.isLineLoop){let v=vl(this,e,_o,c,g-1,p,g-1);v&&n.push(v)}}}updateMorphTargets(){let n=this.geometry.morphAttributes,r=Object.keys(n);if(r.length>0){let s=n[r[0]];if(s!==void 0){this.morphTargetInfluences=[],this.morphTargetDictionary={};for(let o=0,a=s.length;o<a;o++){let l=s[o].name||String(o);this.morphTargetInfluences.push(0),this.morphTargetDictionary[l]=o}}}}};function vl(i,e,n,r,s,o,a){let l=i.geometry.attributes.position;if(kl.fromBufferAttribute(l,s),Bl.fromBufferAttribute(l,o),n.distanceSqToSegment(kl,Bl,nf,Rm)>r)return;nf.applyMatrix4(i.matrixWorld);let u=e.ray.origin.distanceTo(nf);if(!(u<e.near||u>e.far))return{distance:u,point:Rm.clone().applyMatrix4(i.matrixWorld),index:a,face:null,faceIndex:null,barycoord:null,object:i}}var Po=class extends ln{constructor(e=[],n=rr,r,s,o,a,l,c,u,h){super(e,n,r,s,o,a,l,c,u,h),this.isCubeTexture=!0,this.flipY=!1}get images(){return this.image}set images(e){this.image=e}};var Ji=class extends ln{constructor(e,n,r=Kn,s,o,a,l=Nt,c=Nt,u,h=fi,d=1){if(h!==fi&&h!==or)throw new Error("THREE.DepthTexture: format must be either THREE.DepthFormat or THREE.DepthStencilFormat");let f={width:e,height:n,depth:d};super(f,s,o,a,l,c,h,r,u),this.isDepthTexture=!0,this.flipY=!1,this.generateMipmaps=!1,this.compareFunction=null}copy(e){return super.copy(e),this.source=new Ts(Object.assign({},e.image)),this.compareFunction=e.compareFunction,this}toJSON(e){let n=super.toJSON(e);return n.compareFunction=this.compareFunction,n}},zl=class extends Ji{constructor(e,n=Kn,r=rr,s,o,a=Nt,l=Nt,c,u=fi){let h={width:e,height:e,depth:1},d=[h,h,h,h,h,h];super(e,e,n,r,s,o,a,l,c,u),this.image=d,this.isCubeDepthTexture=!0,this.isCubeTexture=!0}get images(){return this.image}set images(e){this.image=e}},Io=class extends ln{constructor(e=null){super(),this.sourceTexture=e,this.isExternalTexture=!0}copy(e){return super.copy(e),this.sourceTexture=e.sourceTexture,this}},Ls=class i extends Ft{constructor(e=1,n=1,r=1,s=1,o=1,a=1){super(),this.type="BoxGeometry",this.parameters={width:e,height:n,depth:r,widthSegments:s,heightSegments:o,depthSegments:a};let l=this;s=Math.floor(s),o=Math.floor(o),a=Math.floor(a);let c=[],u=[],h=[],d=[],f=0,p=0;g("z","y","x",-1,-1,r,n,e,a,o,0),g("z","y","x",1,-1,r,n,-e,a,o,1),g("x","z","y",1,1,e,r,n,s,a,2),g("x","z","y",1,-1,e,r,-n,s,a,3),g("x","y","z",1,-1,e,n,r,s,o,4),g("x","y","z",-1,-1,e,n,-r,s,o,5),this.setIndex(c),this.setAttribute("position",new _t(u,3)),this.setAttribute("normal",new _t(h,3)),this.setAttribute("uv",new _t(d,2));function g(v,_,m,w,T,y,b,S,A,x,C){let L=y/A,P=b/x,O=y/2,U=b/2,M=S/2,I=A+1,D=x+1,k=0,q=0,W=new F;for(let j=0;j<D;j++){let B=j*P-U;for(let Q=0;Q<I;Q++){let re=Q*L-O;W[v]=re*w,W[_]=B*T,W[m]=M,u.push(W.x,W.y,W.z),W[v]=0,W[_]=0,W[m]=S>0?1:-1,h.push(W.x,W.y,W.z),d.push(Q/A),d.push(1-j/x),k+=1}}for(let j=0;j<x;j++)for(let B=0;B<A;B++){let Q=f+B+I*j,re=f+B+I*(j+1),be=f+(B+1)+I*(j+1),ee=f+(B+1)+I*j;c.push(Q,re,ee),c.push(re,be,ee),q+=6}l.addGroup(p,q,C),p+=q,f+=k}}copy(e){return super.copy(e),this.parameters=Object.assign({},e.parameters),this}static fromJSON(e){return new i(e.width,e.height,e.depth,e.widthSegments,e.heightSegments,e.depthSegments)}};var Ds=class i extends Ft{constructor(e=1,n=1,r=1,s=32,o=1,a=!1,l=0,c=Math.PI*2){super(),this.type="CylinderGeometry",this.parameters={radiusTop:e,radiusBottom:n,height:r,radialSegments:s,heightSegments:o,openEnded:a,thetaStart:l,thetaLength:c};let u=this;s=Math.floor(s),o=Math.floor(o);let h=[],d=[],f=[],p=[],g=0,v=[],_=r/2,m=0;w(),a===!1&&(e>0&&T(!0),n>0&&T(!1)),this.setIndex(h),this.setAttribute("position",new _t(d,3)),this.setAttribute("normal",new _t(f,3)),this.setAttribute("uv",new _t(p,2));function w(){let y=new F,b=new F,S=0,A=(n-e)/r;for(let x=0;x<=o;x++){let C=[],L=x/o,P=L*(n-e)+e;for(let O=0;O<=s;O++){let U=O/s,M=U*c+l,I=Math.sin(M),D=Math.cos(M);b.x=P*I,b.y=-L*r+_,b.z=P*D,d.push(b.x,b.y,b.z),y.set(I,A,D).normalize(),f.push(y.x,y.y,y.z),p.push(U,1-L),C.push(g++)}v.push(C)}for(let x=0;x<s;x++)for(let C=0;C<o;C++){let L=v[C][x],P=v[C+1][x],O=v[C+1][x+1],U=v[C][x+1];(e>0||C!==0)&&(h.push(L,P,U),S+=3),(n>0||C!==o-1)&&(h.push(P,O,U),S+=3)}u.addGroup(m,S,0),m+=S}function T(y){let b=g,S=new fe,A=new F,x=0,C=y===!0?e:n,L=y===!0?1:-1;for(let O=1;O<=s;O++)d.push(0,_*L,0),f.push(0,L,0),p.push(.5,.5),g++;let P=g;for(let O=0;O<=s;O++){let M=O/s*c+l,I=Math.cos(M),D=Math.sin(M);A.x=C*D,A.y=_*L,A.z=C*I,d.push(A.x,A.y,A.z),f.push(0,L,0),S.x=I*.5+.5,S.y=D*.5*L+.5,p.push(S.x,S.y),g++}for(let O=0;O<s;O++){let U=b+O,M=P+O;y===!0?h.push(M,M+1,U):h.push(M+1,M,U),x+=3}u.addGroup(m,x,y===!0?1:2),m+=x}}copy(e){return super.copy(e),this.parameters=Object.assign({},e.parameters),this}static fromJSON(e){return new i(e.radiusTop,e.radiusBottom,e.height,e.radialSegments,e.heightSegments,e.openEnded,e.thetaStart,e.thetaLength)}},Lo=class i extends Ds{constructor(e=1,n=1,r=32,s=1,o=!1,a=0,l=Math.PI*2){super(0,e,n,r,s,o,a,l),this.type="ConeGeometry",this.parameters={radius:e,height:n,radialSegments:r,heightSegments:s,openEnded:o,thetaStart:a,thetaLength:l}}static fromJSON(e){return new i(e.radius,e.height,e.radialSegments,e.heightSegments,e.openEnded,e.thetaStart,e.thetaLength)}};var Ln=class{constructor(){this.type="Curve",this.arcLengthDivisions=200,this.needsUpdate=!1,this.cacheArcLengths=null}getPoint(){Ue("Curve: .getPoint() not implemented.")}getPointAt(e,n){let r=this.getUtoTmapping(e);return this.getPoint(r,n)}getPoints(e=5){let n=[];for(let r=0;r<=e;r++)n.push(this.getPoint(r/e));return n}getSpacedPoints(e=5){let n=[];for(let r=0;r<=e;r++)n.push(this.getPointAt(r/e));return n}getLength(){let e=this.getLengths();return e[e.length-1]}getLengths(e=this.arcLengthDivisions){if(this.cacheArcLengths&&this.cacheArcLengths.length===e+1&&!this.needsUpdate)return this.cacheArcLengths;this.needsUpdate=!1;let n=[],r,s=this.getPoint(0),o=0;n.push(0);for(let a=1;a<=e;a++)r=this.getPoint(a/e),o+=r.distanceTo(s),n.push(o),s=r;return this.cacheArcLengths=n,n}updateArcLengths(){this.needsUpdate=!0,this.getLengths()}getUtoTmapping(e,n=null){let r=this.getLengths(),s=0,o=r.length,a;n?a=n:a=e*r[o-1];let l=0,c=o-1,u;for(;l<=c;)if(s=Math.floor(l+(c-l)/2),u=r[s]-a,u<0)l=s+1;else if(u>0)c=s-1;else{c=s;break}if(s=c,r[s]===a)return s/(o-1);let h=r[s],f=r[s+1]-h,p=(a-h)/f;return(s+p)/(o-1)}getTangent(e,n){let s=e-1e-4,o=e+1e-4;s<0&&(s=0),o>1&&(o=1);let a=this.getPoint(s),l=this.getPoint(o),c=n||(a.isVector2?new fe:new F);return c.copy(l).sub(a).normalize(),c}getTangentAt(e,n){let r=this.getUtoTmapping(e);return this.getTangent(r,n)}computeFrenetFrames(e,n=!1){let r=new F,s=[],o=[],a=[],l=new F,c=new st;for(let p=0;p<=e;p++){let g=p/e;s[p]=this.getTangentAt(g,new F)}o[0]=new F,a[0]=new F;let u=Number.MAX_VALUE,h=Math.abs(s[0].x),d=Math.abs(s[0].y),f=Math.abs(s[0].z);h<=u&&(u=h,r.set(1,0,0)),d<=u&&(u=d,r.set(0,1,0)),f<=u&&r.set(0,0,1),l.crossVectors(s[0],r).normalize(),o[0].crossVectors(s[0],l),a[0].crossVectors(s[0],o[0]);for(let p=1;p<=e;p++){if(o[p]=o[p-1].clone(),a[p]=a[p-1].clone(),l.crossVectors(s[p-1],s[p]),l.length()>Number.EPSILON){l.normalize();let g=Math.acos(je(s[p-1].dot(s[p]),-1,1));o[p].applyMatrix4(c.makeRotationAxis(l,g))}a[p].crossVectors(s[p],o[p])}if(n===!0){let p=Math.acos(je(o[0].dot(o[e]),-1,1));p/=e,s[0].dot(l.crossVectors(o[0],o[e]))>0&&(p=-p);for(let g=1;g<=e;g++)o[g].applyMatrix4(c.makeRotationAxis(s[g],p*g)),a[g].crossVectors(s[g],o[g])}return{tangents:s,normals:o,binormals:a}}clone(){return new this.constructor().copy(this)}copy(e){return this.arcLengthDivisions=e.arcLengthDivisions,this}toJSON(){let e={metadata:{version:4.7,type:"Curve",generator:"Curve.toJSON"}};return e.arcLengthDivisions=this.arcLengthDivisions,e.type=this.type,e}fromJSON(e){return this.arcLengthDivisions=e.arcLengthDivisions,this}},Do=class extends Ln{constructor(e=0,n=0,r=1,s=1,o=0,a=Math.PI*2,l=!1,c=0){super(),this.isEllipseCurve=!0,this.type="EllipseCurve",this.aX=e,this.aY=n,this.xRadius=r,this.yRadius=s,this.aStartAngle=o,this.aEndAngle=a,this.aClockwise=l,this.aRotation=c}getPoint(e,n=new fe){let r=n,s=Math.PI*2,o=this.aEndAngle-this.aStartAngle,a=Math.abs(o)<Number.EPSILON;for(;o<0;)o+=s;for(;o>s;)o-=s;o<Number.EPSILON&&(a?o=0:o=s),this.aClockwise===!0&&!a&&(o===s?o=-s:o=o-s);let l=this.aStartAngle+e*o,c=this.aX+this.xRadius*Math.cos(l),u=this.aY+this.yRadius*Math.sin(l);if(this.aRotation!==0){let h=Math.cos(this.aRotation),d=Math.sin(this.aRotation),f=c-this.aX,p=u-this.aY;c=f*h-p*d+this.aX,u=f*d+p*h+this.aY}return r.set(c,u)}copy(e){return super.copy(e),this.aX=e.aX,this.aY=e.aY,this.xRadius=e.xRadius,this.yRadius=e.yRadius,this.aStartAngle=e.aStartAngle,this.aEndAngle=e.aEndAngle,this.aClockwise=e.aClockwise,this.aRotation=e.aRotation,this}toJSON(){let e=super.toJSON();return e.aX=this.aX,e.aY=this.aY,e.xRadius=this.xRadius,e.yRadius=this.yRadius,e.aStartAngle=this.aStartAngle,e.aEndAngle=this.aEndAngle,e.aClockwise=this.aClockwise,e.aRotation=this.aRotation,e}fromJSON(e){return super.fromJSON(e),this.aX=e.aX,this.aY=e.aY,this.xRadius=e.xRadius,this.yRadius=e.yRadius,this.aStartAngle=e.aStartAngle,this.aEndAngle=e.aEndAngle,this.aClockwise=e.aClockwise,this.aRotation=e.aRotation,this}},Vl=class extends Do{constructor(e,n,r,s,o,a){super(e,n,r,r,s,o,a),this.isArcCurve=!0,this.type="ArcCurve"}};function Vf(){let i=0,e=0,n=0,r=0;function s(o,a,l,c){i=o,e=l,n=-3*o+3*a-2*l-c,r=2*o-2*a+l+c}return{initCatmullRom:function(o,a,l,c,u){s(a,l,u*(l-o),u*(c-a))},initNonuniformCatmullRom:function(o,a,l,c,u,h,d){let f=(a-o)/u-(l-o)/(u+h)+(l-a)/h,p=(l-a)/h-(c-a)/(h+d)+(c-l)/d;f*=h,p*=h,s(a,l,f,p)},calc:function(o){let a=o*o,l=a*o;return i+e*o+n*a+r*l}}}var Pm=new F,Im=new F,rf=new Vf,sf=new Vf,of=new Vf,Gl=class extends Ln{constructor(e=[],n=!1,r="centripetal",s=.5){super(),this.isCatmullRomCurve3=!0,this.type="CatmullRomCurve3",this.points=e,this.closed=n,this.curveType=r,this.tension=s}getPoint(e,n=new F){let r=n,s=this.points,o=s.length,a=(o-(this.closed?0:1))*e,l=Math.floor(a),c=a-l;this.closed?l+=l>0?0:(Math.floor(Math.abs(l)/o)+1)*o:c===0&&l===o-1&&(l=o-2,c=1);let u,h;this.closed||l>0?u=s[(l-1)%o]:(Im.subVectors(s[0],s[1]).add(s[0]),u=Im);let d=s[l%o],f=s[(l+1)%o];if(this.closed||l+2<o?h=s[(l+2)%o]:(Pm.subVectors(s[o-1],s[o-2]).add(s[o-1]),h=Pm),this.curveType==="centripetal"||this.curveType==="chordal"){let p=this.curveType==="chordal"?.5:.25,g=Math.pow(u.distanceToSquared(d),p),v=Math.pow(d.distanceToSquared(f),p),_=Math.pow(f.distanceToSquared(h),p);v<1e-4&&(v=1),g<1e-4&&(g=v),_<1e-4&&(_=v),rf.initNonuniformCatmullRom(u.x,d.x,f.x,h.x,g,v,_),sf.initNonuniformCatmullRom(u.y,d.y,f.y,h.y,g,v,_),of.initNonuniformCatmullRom(u.z,d.z,f.z,h.z,g,v,_)}else this.curveType==="catmullrom"&&(rf.initCatmullRom(u.x,d.x,f.x,h.x,this.tension),sf.initCatmullRom(u.y,d.y,f.y,h.y,this.tension),of.initCatmullRom(u.z,d.z,f.z,h.z,this.tension));return r.set(rf.calc(c),sf.calc(c),of.calc(c)),r}copy(e){super.copy(e),this.points=[];for(let n=0,r=e.points.length;n<r;n++){let s=e.points[n];this.points.push(s.clone())}return this.closed=e.closed,this.curveType=e.curveType,this.tension=e.tension,this}toJSON(){let e=super.toJSON();e.points=[];for(let n=0,r=this.points.length;n<r;n++){let s=this.points[n];e.points.push(s.toArray())}return e.closed=this.closed,e.curveType=this.curveType,e.tension=this.tension,e}fromJSON(e){super.fromJSON(e),this.points=[];for(let n=0,r=e.points.length;n<r;n++){let s=e.points[n];this.points.push(new F().fromArray(s))}return this.closed=e.closed,this.curveType=e.curveType,this.tension=e.tension,this}};function Lm(i,e,n,r,s){let o=(r-e)*.5,a=(s-n)*.5,l=i*i,c=i*l;return(2*n-2*r+o+a)*c+(-3*n+3*r-2*o-a)*l+o*i+n}function HS(i,e){let n=1-i;return n*n*e}function WS(i,e){return 2*(1-i)*i*e}function XS(i,e){return i*i*e}function xo(i,e,n,r){return HS(i,e)+WS(i,n)+XS(i,r)}function qS(i,e){let n=1-i;return n*n*n*e}function jS(i,e){let n=1-i;return 3*n*n*i*e}function YS(i,e){return 3*(1-i)*i*i*e}function $S(i,e){return i*i*i*e}function bo(i,e,n,r,s){return qS(i,e)+jS(i,n)+YS(i,r)+$S(i,s)}var Hl=class extends Ln{constructor(e=new fe,n=new fe,r=new fe,s=new fe){super(),this.isCubicBezierCurve=!0,this.type="CubicBezierCurve",this.v0=e,this.v1=n,this.v2=r,this.v3=s}getPoint(e,n=new fe){let r=n,s=this.v0,o=this.v1,a=this.v2,l=this.v3;return r.set(bo(e,s.x,o.x,a.x,l.x),bo(e,s.y,o.y,a.y,l.y)),r}copy(e){return super.copy(e),this.v0.copy(e.v0),this.v1.copy(e.v1),this.v2.copy(e.v2),this.v3.copy(e.v3),this}toJSON(){let e=super.toJSON();return e.v0=this.v0.toArray(),e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e.v3=this.v3.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v0.fromArray(e.v0),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this.v3.fromArray(e.v3),this}},Os=class extends Ln{constructor(e=new F,n=new F,r=new F,s=new F){super(),this.isCubicBezierCurve3=!0,this.type="CubicBezierCurve3",this.v0=e,this.v1=n,this.v2=r,this.v3=s}getPoint(e,n=new F){let r=n,s=this.v0,o=this.v1,a=this.v2,l=this.v3;return r.set(bo(e,s.x,o.x,a.x,l.x),bo(e,s.y,o.y,a.y,l.y),bo(e,s.z,o.z,a.z,l.z)),r}copy(e){return super.copy(e),this.v0.copy(e.v0),this.v1.copy(e.v1),this.v2.copy(e.v2),this.v3.copy(e.v3),this}toJSON(){let e=super.toJSON();return e.v0=this.v0.toArray(),e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e.v3=this.v3.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v0.fromArray(e.v0),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this.v3.fromArray(e.v3),this}},Wl=class extends Ln{constructor(e=new fe,n=new fe){super(),this.isLineCurve=!0,this.type="LineCurve",this.v1=e,this.v2=n}getPoint(e,n=new fe){let r=n;return e===1?r.copy(this.v2):(r.copy(this.v2).sub(this.v1),r.multiplyScalar(e).add(this.v1)),r}getPointAt(e,n){return this.getPoint(e,n)}getTangent(e,n=new fe){return n.subVectors(this.v2,this.v1).normalize()}getTangentAt(e,n){return this.getTangent(e,n)}copy(e){return super.copy(e),this.v1.copy(e.v1),this.v2.copy(e.v2),this}toJSON(){let e=super.toJSON();return e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this}},Xl=class extends Ln{constructor(e=new F,n=new F){super(),this.isLineCurve3=!0,this.type="LineCurve3",this.v1=e,this.v2=n}getPoint(e,n=new F){let r=n;return e===1?r.copy(this.v2):(r.copy(this.v2).sub(this.v1),r.multiplyScalar(e).add(this.v1)),r}getPointAt(e,n){return this.getPoint(e,n)}getTangent(e,n=new F){return n.subVectors(this.v2,this.v1).normalize()}getTangentAt(e,n){return this.getTangent(e,n)}copy(e){return super.copy(e),this.v1.copy(e.v1),this.v2.copy(e.v2),this}toJSON(){let e=super.toJSON();return e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this}},ql=class extends Ln{constructor(e=new fe,n=new fe,r=new fe){super(),this.isQuadraticBezierCurve=!0,this.type="QuadraticBezierCurve",this.v0=e,this.v1=n,this.v2=r}getPoint(e,n=new fe){let r=n,s=this.v0,o=this.v1,a=this.v2;return r.set(xo(e,s.x,o.x,a.x),xo(e,s.y,o.y,a.y)),r}copy(e){return super.copy(e),this.v0.copy(e.v0),this.v1.copy(e.v1),this.v2.copy(e.v2),this}toJSON(){let e=super.toJSON();return e.v0=this.v0.toArray(),e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v0.fromArray(e.v0),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this}},Lr=class extends Ln{constructor(e=new F,n=new F,r=new F){super(),this.isQuadraticBezierCurve3=!0,this.type="QuadraticBezierCurve3",this.v0=e,this.v1=n,this.v2=r}getPoint(e,n=new F){let r=n,s=this.v0,o=this.v1,a=this.v2;return r.set(xo(e,s.x,o.x,a.x),xo(e,s.y,o.y,a.y),xo(e,s.z,o.z,a.z)),r}copy(e){return super.copy(e),this.v0.copy(e.v0),this.v1.copy(e.v1),this.v2.copy(e.v2),this}toJSON(){let e=super.toJSON();return e.v0=this.v0.toArray(),e.v1=this.v1.toArray(),e.v2=this.v2.toArray(),e}fromJSON(e){return super.fromJSON(e),this.v0.fromArray(e.v0),this.v1.fromArray(e.v1),this.v2.fromArray(e.v2),this}},jl=class extends Ln{constructor(e=[]){super(),this.isSplineCurve=!0,this.type="SplineCurve",this.points=e}getPoint(e,n=new fe){let r=n,s=this.points,o=(s.length-1)*e,a=Math.floor(o),l=o-a,c=s[a===0?a:a-1],u=s[a],h=s[a>s.length-2?s.length-1:a+1],d=s[a>s.length-3?s.length-1:a+2];return r.set(Lm(l,c.x,u.x,h.x,d.x),Lm(l,c.y,u.y,h.y,d.y)),r}copy(e){super.copy(e),this.points=[];for(let n=0,r=e.points.length;n<r;n++){let s=e.points[n];this.points.push(s.clone())}return this}toJSON(){let e=super.toJSON();e.points=[];for(let n=0,r=this.points.length;n<r;n++){let s=this.points[n];e.points.push(s.toArray())}return e}fromJSON(e){super.fromJSON(e),this.points=[];for(let n=0,r=e.points.length;n<r;n++){let s=e.points[n];this.points.push(new fe().fromArray(s))}return this}},ZS=Object.freeze({__proto__:null,ArcCurve:Vl,CatmullRomCurve3:Gl,CubicBezierCurve:Hl,CubicBezierCurve3:Os,EllipseCurve:Do,LineCurve:Wl,LineCurve3:Xl,QuadraticBezierCurve:ql,QuadraticBezierCurve3:Lr,SplineCurve:jl});var Oo=class i extends Ft{constructor(e=1,n=1,r=1,s=1){super(),this.type="PlaneGeometry",this.parameters={width:e,height:n,widthSegments:r,heightSegments:s};let o=e/2,a=n/2,l=Math.floor(r),c=Math.floor(s),u=l+1,h=c+1,d=e/l,f=n/c,p=[],g=[],v=[],_=[];for(let m=0;m<h;m++){let w=m*f-a;for(let T=0;T<u;T++){let y=T*d-o;g.push(y,-w,0),v.push(0,0,1),_.push(T/l),_.push(1-m/c)}}for(let m=0;m<c;m++)for(let w=0;w<l;w++){let T=w+u*m,y=w+u*(m+1),b=w+1+u*(m+1),S=w+1+u*m;p.push(T,y,S),p.push(y,b,S)}this.setIndex(p),this.setAttribute("position",new _t(g,3)),this.setAttribute("normal",new _t(v,3)),this.setAttribute("uv",new _t(_,2))}copy(e){return super.copy(e),this.parameters=Object.assign({},e.parameters),this}static fromJSON(e){return new i(e.width,e.height,e.widthSegments,e.heightSegments)}};var Dr=class i extends Ft{constructor(e=1,n=32,r=16,s=0,o=Math.PI*2,a=0,l=Math.PI){super(),this.type="SphereGeometry",this.parameters={radius:e,widthSegments:n,heightSegments:r,phiStart:s,phiLength:o,thetaStart:a,thetaLength:l},n=Math.max(3,Math.floor(n)),r=Math.max(2,Math.floor(r));let c=Math.min(a+l,Math.PI),u=0,h=[],d=new F,f=new F,p=[],g=[],v=[],_=[];for(let m=0;m<=r;m++){let w=[],T=m/r,y=a+T*l,b=e*Math.cos(y),S=Math.sqrt(e*e-b*b),A=0;m===0&&a===0?A=.5/n:m===r&&c===Math.PI&&(A=-.5/n);for(let x=0;x<=n;x++){let C=x/n,L=s+C*o;d.x=-S*Math.cos(L),d.y=b,d.z=S*Math.sin(L),g.push(d.x,d.y,d.z),f.copy(d).normalize(),v.push(f.x,f.y,f.z),_.push(C+A,1-T),w.push(u++)}h.push(w)}for(let m=0;m<r;m++)for(let w=0;w<n;w++){let T=h[m][w+1],y=h[m][w],b=h[m+1][w],S=h[m+1][w+1];(m!==0||a>0)&&p.push(T,y,S),(m!==r-1||c<Math.PI)&&p.push(y,b,S)}this.setIndex(p),this.setAttribute("position",new _t(g,3)),this.setAttribute("normal",new _t(v,3)),this.setAttribute("uv",new _t(_,2))}copy(e){return super.copy(e),this.parameters=Object.assign({},e.parameters),this}static fromJSON(e){return new i(e.radius,e.widthSegments,e.heightSegments,e.phiStart,e.phiLength,e.thetaStart,e.thetaLength)}};var No=class i extends Ft{constructor(e=new Lr(new F(-1,-1,0),new F(-1,1,0),new F(1,1,0)),n=64,r=1,s=8,o=!1){super(),this.type="TubeGeometry",this.parameters={path:e,tubularSegments:n,radius:r,radialSegments:s,closed:o};let a=e.computeFrenetFrames(n,o);this.tangents=a.tangents,this.normals=a.normals,this.binormals=a.binormals;let l=new F,c=new F,u=new fe,h=new F,d=[],f=[],p=[],g=[];v(),this.setIndex(g),this.setAttribute("position",new _t(d,3)),this.setAttribute("normal",new _t(f,3)),this.setAttribute("uv",new _t(p,2));function v(){for(let T=0;T<n;T++)_(T);_(o===!1?n:0),w(),m()}function _(T){h=e.getPointAt(T/n,h);let y=a.normals[T],b=a.binormals[T];for(let S=0;S<=s;S++){let A=S/s*Math.PI*2,x=Math.sin(A),C=-Math.cos(A);c.x=C*y.x+x*b.x,c.y=C*y.y+x*b.y,c.z=C*y.z+x*b.z,c.normalize(),f.push(c.x,c.y,c.z),l.x=h.x+r*c.x,l.y=h.y+r*c.y,l.z=h.z+r*c.z,d.push(l.x,l.y,l.z)}}function m(){for(let T=1;T<=n;T++)for(let y=1;y<=s;y++){let b=(s+1)*(T-1)+(y-1),S=(s+1)*T+(y-1),A=(s+1)*T+y,x=(s+1)*(T-1)+y;g.push(b,S,x),g.push(S,A,x)}}function w(){for(let T=0;T<=n;T++)for(let y=0;y<=s;y++)u.x=T/n,u.y=y/s,p.push(u.x,u.y)}}copy(e){return super.copy(e),this.parameters=Object.assign({},e.parameters),this}toJSON(){let e=super.toJSON();return e.path=this.parameters.path.toJSON(),e}static fromJSON(e){return new i(new ZS[e.path.type]().fromJSON(e.path),e.tubularSegments,e.radius,e.radialSegments,e.closed)}};function kr(i){let e={};for(let n in i){e[n]={};for(let r in i[n]){let s=i[n][r];if(Dm(s))s.isRenderTargetTexture?(Ue("UniformsUtils: Textures of render targets cannot be cloned via cloneUniforms() or mergeUniforms()."),e[n][r]=null):e[n][r]=s.clone();else if(Array.isArray(s))if(Dm(s[0])){let o=[];for(let a=0,l=s.length;a<l;a++)o[a]=s[a].clone();e[n][r]=o}else e[n][r]=s.slice();else e[n][r]=s}}return e}function Qt(i){let e={};for(let n=0;n<i.length;n++){let r=kr(i[n]);for(let s in r)e[s]=r[s]}return e}function Dm(i){return i&&(i.isColor||i.isMatrix3||i.isMatrix4||i.isVector2||i.isVector3||i.isVector4||i.isTexture||i.isQuaternion)}function KS(i){let e=[];for(let n=0;n<i.length;n++)e.push(i[n].clone());return e}function Gf(i){let e=i.getRenderTarget();return e===null?i.outputColorSpace:e.isXRRenderTarget===!0?e.texture.colorSpace:Ke.workingColorSpace}var eu={clone:kr,merge:Qt},JS=`void main() {
	gl_Position = projectionMatrix * modelViewMatrix * vec4( position, 1.0 );
}`,QS=`void main() {
	gl_FragColor = vec4( 1.0, 0.0, 0.0, 1.0 );
}`,Jt=class extends Ri{constructor(e){super(),this.isShaderMaterial=!0,this.type="ShaderMaterial",this.defines={},this.uniforms={},this.uniformsGroups=[],this.vertexShader=JS,this.fragmentShader=QS,this.linewidth=1,this.wireframe=!1,this.wireframeLinewidth=1,this.fog=!1,this.lights=!1,this.clipping=!1,this.forceSinglePass=!0,this.extensions={clipCullDistance:!1,multiDraw:!1},this.defaultAttributeValues={color:[1,1,1],uv:[0,0],uv1:[0,0]},this.index0AttributeName=void 0,this.uniformsNeedUpdate=!1,this.glslVersion=null,e!==void 0&&this.setValues(e)}copy(e){return super.copy(e),this.fragmentShader=e.fragmentShader,this.vertexShader=e.vertexShader,this.uniforms=kr(e.uniforms),this.uniformsGroups=KS(e.uniformsGroups),this.defines=Object.assign({},e.defines),this.wireframe=e.wireframe,this.wireframeLinewidth=e.wireframeLinewidth,this.fog=e.fog,this.lights=e.lights,this.clipping=e.clipping,this.extensions=Object.assign({},e.extensions),this.glslVersion=e.glslVersion,this.defaultAttributeValues=Object.assign({},e.defaultAttributeValues),this.index0AttributeName=e.index0AttributeName,this.uniformsNeedUpdate=e.uniformsNeedUpdate,this}toJSON(e){let n=super.toJSON(e);n.glslVersion=this.glslVersion,n.uniforms={};for(let s in this.uniforms){let a=this.uniforms[s].value;a&&a.isTexture?n.uniforms[s]={type:"t",value:a.toJSON(e).uuid}:a&&a.isColor?n.uniforms[s]={type:"c",value:a.getHex()}:a&&a.isVector2?n.uniforms[s]={type:"v2",value:a.toArray()}:a&&a.isVector3?n.uniforms[s]={type:"v3",value:a.toArray()}:a&&a.isVector4?n.uniforms[s]={type:"v4",value:a.toArray()}:a&&a.isMatrix3?n.uniforms[s]={type:"m3",value:a.toArray()}:a&&a.isMatrix4?n.uniforms[s]={type:"m4",value:a.toArray()}:n.uniforms[s]={value:a}}Object.keys(this.defines).length>0&&(n.defines=this.defines),n.vertexShader=this.vertexShader,n.fragmentShader=this.fragmentShader,n.lights=this.lights,n.clipping=this.clipping;let r={};for(let s in this.extensions)this.extensions[s]===!0&&(r[s]=!0);return Object.keys(r).length>0&&(n.extensions=r),n}fromJSON(e,n){if(super.fromJSON(e,n),e.uniforms!==void 0)for(let r in e.uniforms){let s=e.uniforms[r];switch(this.uniforms[r]={},s.type){case"t":this.uniforms[r].value=n[s.value]||null;break;case"c":this.uniforms[r].value=new We().setHex(s.value);break;case"v2":this.uniforms[r].value=new fe().fromArray(s.value);break;case"v3":this.uniforms[r].value=new F().fromArray(s.value);break;case"v4":this.uniforms[r].value=new vt().fromArray(s.value);break;case"m3":this.uniforms[r].value=new He().fromArray(s.value);break;case"m4":this.uniforms[r].value=new st().fromArray(s.value);break;default:this.uniforms[r].value=s.value}}if(e.defines!==void 0&&(this.defines=e.defines),e.vertexShader!==void 0&&(this.vertexShader=e.vertexShader),e.fragmentShader!==void 0&&(this.fragmentShader=e.fragmentShader),e.glslVersion!==void 0&&(this.glslVersion=e.glslVersion),e.extensions!==void 0)for(let r in e.extensions)this.extensions[r]=e.extensions[r];return e.lights!==void 0&&(this.lights=e.lights),e.clipping!==void 0&&(this.clipping=e.clipping),this}},Yl=class extends Jt{constructor(e){super(e),this.isRawShaderMaterial=!0,this.type="RawShaderMaterial"}};var Uo=class extends Ri{constructor(e){super(),this.isMeshLambertMaterial=!0,this.type="MeshLambertMaterial",this.color=new We(16777215),this.map=null,this.lightMap=null,this.lightMapIntensity=1,this.aoMap=null,this.aoMapIntensity=1,this.emissive=new We(0),this.emissiveIntensity=1,this.emissiveMap=null,this.bumpMap=null,this.bumpScale=1,this.normalMap=null,this.normalMapType=Kc,this.normalScale=new fe(1,1),this.displacementMap=null,this.displacementScale=1,this.displacementBias=0,this.specularMap=null,this.alphaMap=null,this.envMap=null,this.envMapRotation=new Ci,this.combine=hc,this.reflectivity=1,this.envMapIntensity=1,this.refractionRatio=.98,this.wireframe=!1,this.wireframeLinewidth=1,this.wireframeLinecap="round",this.wireframeLinejoin="round",this.flatShading=!1,this.fog=!0,this.setValues(e)}copy(e){return super.copy(e),this.color.copy(e.color),this.map=e.map,this.lightMap=e.lightMap,this.lightMapIntensity=e.lightMapIntensity,this.aoMap=e.aoMap,this.aoMapIntensity=e.aoMapIntensity,this.emissive.copy(e.emissive),this.emissiveMap=e.emissiveMap,this.emissiveIntensity=e.emissiveIntensity,this.bumpMap=e.bumpMap,this.bumpScale=e.bumpScale,this.normalMap=e.normalMap,this.normalMapType=e.normalMapType,this.normalScale.copy(e.normalScale),this.displacementMap=e.displacementMap,this.displacementScale=e.displacementScale,this.displacementBias=e.displacementBias,this.specularMap=e.specularMap,this.alphaMap=e.alphaMap,this.envMap=e.envMap,this.envMapRotation.copy(e.envMapRotation),this.combine=e.combine,this.reflectivity=e.reflectivity,this.envMapIntensity=e.envMapIntensity,this.refractionRatio=e.refractionRatio,this.wireframe=e.wireframe,this.wireframeLinewidth=e.wireframeLinewidth,this.wireframeLinecap=e.wireframeLinecap,this.wireframeLinejoin=e.wireframeLinejoin,this.flatShading=e.flatShading,this.fog=e.fog,this}},$l=class extends Ri{constructor(e){super(),this.isMeshDepthMaterial=!0,this.type="MeshDepthMaterial",this.depthPacking=fg,this.map=null,this.alphaMap=null,this.displacementMap=null,this.displacementScale=1,this.displacementBias=0,this.wireframe=!1,this.wireframeLinewidth=1,this.setValues(e)}copy(e){return super.copy(e),this.depthPacking=e.depthPacking,this.map=e.map,this.alphaMap=e.alphaMap,this.displacementMap=e.displacementMap,this.displacementScale=e.displacementScale,this.displacementBias=e.displacementBias,this.wireframe=e.wireframe,this.wireframeLinewidth=e.wireframeLinewidth,this}},Zl=class extends Ri{constructor(e){super(),this.isMeshDistanceMaterial=!0,this.type="MeshDistanceMaterial",this.map=null,this.alphaMap=null,this.displacementMap=null,this.displacementScale=1,this.displacementBias=0,this.setValues(e)}copy(e){return super.copy(e),this.map=e.map,this.alphaMap=e.alphaMap,this.displacementMap=e.displacementMap,this.displacementScale=e.displacementScale,this.displacementBias=e.displacementBias,this}};function gs(i,e){return!i||i.constructor===e?i:typeof e.BYTES_PER_ELEMENT=="number"?new e(i):Array.prototype.slice.call(i)}function af(i){return i!==void 0&&i.inTangents!==void 0&&i.outTangents!==void 0}var Qi=class{constructor(e,n,r,s){this.parameterPositions=e,this._cachedIndex=0,this.resultBuffer=s!==void 0?s:new n.constructor(r),this.sampleValues=n,this.valueSize=r,this.settings=null,this.DefaultSettings_={}}evaluate(e){let n=this.parameterPositions,r=this._cachedIndex,s=n[r],o=n[r-1];e:{t:{let a;n:{i:if(!(e<s)){for(let l=r+2;;){if(s===void 0){if(e<o)break i;return r=n.length,this._cachedIndex=r,this.copySampleValue_(r-1)}if(r===l)break;if(o=s,s=n[++r],e<s)break t}a=n.length;break n}if(!(e>=o)){let l=n[1];e<l&&(r=2,o=l);for(let c=r-2;;){if(o===void 0)return this._cachedIndex=0,this.copySampleValue_(0);if(r===c)break;if(s=o,o=n[--r-1],e>=o)break t}a=r,r=0;break n}break e}for(;r<a;){let l=r+a>>>1;e<n[l]?a=l:r=l+1}if(s=n[r],o=n[r-1],o===void 0)return this._cachedIndex=0,this.copySampleValue_(0);if(s===void 0)return r=n.length,this._cachedIndex=r,this.copySampleValue_(r-1)}this._cachedIndex=r,this.intervalChanged_(r,o,s)}return this.interpolate_(r,o,e,s)}getSettings_(){return this.settings||this.DefaultSettings_}copySampleValue_(e){let n=this.resultBuffer,r=this.sampleValues,s=this.valueSize,o=e*s;for(let a=0;a!==s;++a)n[a]=r[o+a];return n}interpolate_(){throw new Error("THREE.Interpolant: Call to abstract method.")}intervalChanged_(){}},Kl=class extends Qi{constructor(e,n,r,s){super(e,n,r,s),this._weightPrev=-0,this._offsetPrev=-0,this._weightNext=-0,this._offsetNext=-0,this.DefaultSettings_={endingStart:uf,endingEnd:uf}}intervalChanged_(e,n,r){let s=this.parameterPositions,o=e-2,a=e+1,l=s[o],c=s[a];if(l===void 0)switch(this.getSettings_().endingStart){case hf:o=e,l=2*n-r;break;case ff:o=s.length-2,l=n+s[o]-s[o+1];break;default:o=e,l=r}if(c===void 0)switch(this.getSettings_().endingEnd){case hf:a=e,c=2*r-n;break;case ff:a=1,c=r+s[1]-s[0];break;default:a=e-1,c=n}let u=(r-n)*.5,h=this.valueSize;this._weightPrev=u/(n-l),this._weightNext=u/(c-r),this._offsetPrev=o*h,this._offsetNext=a*h}interpolate_(e,n,r,s){let o=this.resultBuffer,a=this.sampleValues,l=this.valueSize,c=e*l,u=c-l,h=this._offsetPrev,d=this._offsetNext,f=this._weightPrev,p=this._weightNext,g=(r-n)/(s-n),v=g*g,_=v*g,m=-f*_+2*f*v-f*g,w=(1+f)*_+(-1.5-2*f)*v+(-.5+f)*g+1,T=(-1-p)*_+(1.5+p)*v+.5*g,y=p*_-p*v;for(let b=0;b!==l;++b)o[b]=m*a[h+b]+w*a[u+b]+T*a[c+b]+y*a[d+b];return o}},Jl=class extends Qi{constructor(e,n,r,s){super(e,n,r,s)}interpolate_(e,n,r,s){let o=this.resultBuffer,a=this.sampleValues,l=this.valueSize,c=e*l,u=c-l,h=(r-n)/(s-n),d=1-h;for(let f=0;f!==l;++f)o[f]=a[u+f]*d+a[c+f]*h;return o}},Ql=class extends Qi{constructor(e,n,r,s){super(e,n,r,s)}interpolate_(e){return this.copySampleValue_(e-1)}},ec=class extends Qi{interpolate_(e,n,r,s){let o=this.resultBuffer,a=this.sampleValues,l=this.valueSize,c=e*l,u=c-l,h=this.inTangents,d=this.outTangents;if(!h||!d){let g=(r-n)/(s-n),v=1-g;for(let _=0;_!==l;++_)o[_]=a[u+_]*v+a[c+_]*g;return o}let f=l*2,p=e-1;for(let g=0;g!==l;++g){let v=a[u+g],_=a[c+g],m=p*f+g*2,w=d[m],T=d[m+1],y=e*f+g*2,b=h[y],S=h[y+1],A=tw(r,n,w,b,s);o[g]=Tg(A,v,T,S,_)}return o}};function Tg(i,e,n,r,s){let o=1-i;return o*o*o*e+3*o*o*i*n+3*o*i*i*r+i*i*i*s}function ew(i,e,n,r,s){let o=1-i;return 3*o*o*(n-e)+6*o*i*(r-n)+3*i*i*(s-r)}function tw(i,e,n,r,s){let o=(i-e)/(s-e);for(let a=0;a<8;a++){let l=Tg(o,e,n,r,s)-i;if(Math.abs(l)<1e-10)break;let c=ew(o,e,n,r,s);if(Math.abs(c)<1e-10)break;o=Math.max(0,Math.min(1,o-l/c))}return o}var Mn=class{constructor(e,n,r,s){if(e===void 0)throw new Error("THREE.KeyframeTrack: track name is undefined");if(n===void 0||n.length===0)throw new Error("THREE.KeyframeTrack: no keyframes in track named "+e);this.name=e,this.times=gs(n,this.TimeBufferType),this.values=gs(r,this.ValueBufferType),this.setInterpolation(s||this.DefaultInterpolation)}static toJSON(e){let n=e.constructor,r;if(n.toJSON!==this.toJSON)r=n.toJSON(e);else{r={name:e.name,times:gs(e.times,Array),values:gs(e.values,Array)};let s=e.getInterpolation();s!==e.DefaultInterpolation&&(r.interpolation=s),af(e.settings)&&(r.settings={inTangents:gs(e.settings.inTangents,Array),outTangents:gs(e.settings.outTangents,Array)})}return r.type=e.ValueTypeName,r}InterpolantFactoryMethodDiscrete(e){return new Ql(this.times,this.values,this.getValueSize(),e)}InterpolantFactoryMethodLinear(e){return new Jl(this.times,this.values,this.getValueSize(),e)}InterpolantFactoryMethodSmooth(e){return new Kl(this.times,this.values,this.getValueSize(),e)}InterpolantFactoryMethodBezier(e){let n=new ec(this.times,this.values,this.getValueSize(),e);return this.settings&&(n.inTangents=this.settings.inTangents,n.outTangents=this.settings.outTangents),n}setInterpolation(e){let n;switch(e){case So:n=this.InterpolantFactoryMethodDiscrete;break;case Dl:n=this.InterpolantFactoryMethodLinear;break;case bl:n=this.InterpolantFactoryMethodSmooth;break;case cf:n=this.InterpolantFactoryMethodBezier;break}if(n===void 0){let r="unsupported interpolation for "+this.ValueTypeName+" keyframe track named "+this.name;if(this.createInterpolant===void 0)if(e!==this.DefaultInterpolation)this.setInterpolation(this.DefaultInterpolation);else throw new Error(r);return Ue("KeyframeTrack:",r),this}return this.createInterpolant=n,this}getInterpolation(){switch(this.createInterpolant){case this.InterpolantFactoryMethodDiscrete:return So;case this.InterpolantFactoryMethodLinear:return Dl;case this.InterpolantFactoryMethodSmooth:return bl;case this.InterpolantFactoryMethodBezier:return cf}}getValueSize(){return this.values.length/this.times.length}shift(e){if(e!==0){let n=this.times;for(let r=0,s=n.length;r!==s;++r)n[r]+=e}return this}scale(e){if(e!==1){let n=this.times;for(let r=0,s=n.length;r!==s;++r)n[r]*=e;af(this.settings)&&(Om(this.settings.inTangents,e),Om(this.settings.outTangents,e))}return this}trim(e,n){let r=this.times,s=r.length,o=0,a=s-1;for(;o!==s&&r[o]<e;)++o;for(;a!==-1&&r[a]>n;)--a;if(++a,o!==0||a!==s){o>=a&&(a=Math.max(a,1),o=a-1);let l=this.getValueSize();this.times=r.slice(o,a),this.values=this.values.slice(o*l,a*l)}return this}validate(){let e=!0,n=this.getValueSize();n-Math.floor(n)!==0&&(ze("KeyframeTrack: Invalid value size in track.",this),e=!1);let r=this.times,s=this.values,o=r.length;o===0&&(ze("KeyframeTrack: Track is empty.",this),e=!1);let a=null;for(let l=0;l!==o;l++){let c=r[l];if(typeof c=="number"&&isNaN(c)){ze("KeyframeTrack: Time is not a valid number.",this,l,c),e=!1;break}if(a!==null&&a>c){ze("KeyframeTrack: Out of order keys.",this,l,c,a),e=!1;break}a=c}if(s!==void 0&&lS(s))for(let l=0,c=s.length;l!==c;++l){let u=s[l];if(isNaN(u)){ze("KeyframeTrack: Value is not a valid number.",this,l,u),e=!1;break}}return e}optimize(){let e=this.times.slice(),n=this.values.slice(),r=this.getValueSize(),s=this.getInterpolation()===bl,o=e.length-1,a=1;for(let l=1;l<o;++l){let c=!1,u=e[l],h=e[l+1];if(u!==h&&(l!==1||u!==e[0]))if(s)c=!0;else{let d=l*r,f=d-r,p=d+r;for(let g=0;g!==r;++g){let v=n[d+g];if(v!==n[f+g]||v!==n[p+g]){c=!0;break}}}if(c){if(l!==a){e[a]=e[l];let d=l*r,f=a*r;for(let p=0;p!==r;++p)n[f+p]=n[d+p]}++a}}if(o>0){e[a]=e[o];for(let l=o*r,c=a*r,u=0;u!==r;++u)n[c+u]=n[l+u];++a}return a!==e.length?(this.times=e.slice(0,a),this.values=n.slice(0,a*r)):(this.times=e,this.values=n),this}clone(){let e=this.times.slice(),n=this.values.slice(),r=this.constructor,s=new r(this.name,e,n);return s.createInterpolant=this.createInterpolant,af(this.settings)&&(s.settings={inTangents:this.settings.inTangents.slice(),outTangents:this.settings.outTangents.slice()}),s}};function Om(i,e){for(let n=0,r=i.length;n!==r;n+=2)i[n]*=e}Mn.prototype.ValueTypeName="";Mn.prototype.TimeBufferType=Float32Array;Mn.prototype.ValueBufferType=Float32Array;Mn.prototype.DefaultInterpolation=Dl;var er=class extends Mn{constructor(e,n,r){super(e,n,r)}};er.prototype.ValueTypeName="bool";er.prototype.ValueBufferType=Array;er.prototype.DefaultInterpolation=So;er.prototype.InterpolantFactoryMethodLinear=void 0;er.prototype.InterpolantFactoryMethodSmooth=void 0;var tc=class extends Mn{constructor(e,n,r,s){super(e,n,r,s)}};tc.prototype.ValueTypeName="color";var nc=class extends Mn{constructor(e,n,r,s){super(e,n,r,s)}};nc.prototype.ValueTypeName="number";var ic=class extends Qi{constructor(e,n,r,s){super(e,n,r,s)}interpolate_(e,n,r,s){let o=this.resultBuffer,a=this.sampleValues,l=this.valueSize,c=(r-n)/(s-n),u=e*l;for(let h=u+l;u!==h;u+=4)Ut.slerpFlat(o,0,a,u-l,a,u,c);return o}},Fo=class extends Mn{constructor(e,n,r,s){super(e,n,r,s)}InterpolantFactoryMethodLinear(e){return new ic(this.times,this.values,this.getValueSize(),e)}};Fo.prototype.ValueTypeName="quaternion";Fo.prototype.InterpolantFactoryMethodSmooth=void 0;var tr=class extends Mn{constructor(e,n,r){super(e,n,r)}};tr.prototype.ValueTypeName="string";tr.prototype.ValueBufferType=Array;tr.prototype.DefaultInterpolation=So;tr.prototype.InterpolantFactoryMethodLinear=void 0;tr.prototype.InterpolantFactoryMethodSmooth=void 0;var rc=class extends Mn{constructor(e,n,r,s){super(e,n,r,s)}};rc.prototype.ValueTypeName="vector";var wl={enabled:!1,files:{},add:function(i,e){this.enabled!==!1&&(Nm(i)||(this.files[i]=e))},get:function(i){if(this.enabled!==!1&&!Nm(i))return this.files[i]},remove:function(i){delete this.files[i]},clear:function(){this.files={}}};function Nm(i){try{let e=i.slice(i.indexOf(":")+1);return new URL(e).protocol==="blob:"}catch{return!1}}var sc=class{constructor(e,n,r){let s=this,o=!1,a=0,l=0,c,u=[];this.onStart=void 0,this.onLoad=e,this.onProgress=n,this.onError=r,this._abortController=null,this.itemStart=function(h){l++,o===!1&&s.onStart!==void 0&&s.onStart(h,a,l),o=!0},this.itemEnd=function(h){a++,s.onProgress!==void 0&&s.onProgress(h,a,l),a===l&&(o=!1,s.onLoad!==void 0&&s.onLoad())},this.itemError=function(h){s.onError!==void 0&&s.onError(h)},this.resolveURL=function(h){return h=h.normalize("NFC"),c?c(h):h},this.setURLModifier=function(h){return c=h,this},this.addHandler=function(h,d){return u.push(h,d),this},this.removeHandler=function(h){let d=u.indexOf(h);return d!==-1&&u.splice(d,2),this},this.getHandler=function(h){for(let d=0,f=u.length;d<f;d+=2){let p=u[d],g=u[d+1];if(p.global&&(p.lastIndex=0),p.test(h))return g}return null},this.abort=function(){return this.abortController.abort(),this._abortController=null,this}}get abortController(){return this._abortController||(this._abortController=new AbortController),this._abortController}},Cg=new sc,Ns=class{constructor(e){this.manager=e!==void 0?e:Cg,this.crossOrigin="anonymous",this.withCredentials=!1,this.path="",this.resourcePath="",this.requestHeader={},typeof __THREE_DEVTOOLS__<"u"&&__THREE_DEVTOOLS__.dispatchEvent(new CustomEvent("observe",{detail:this}))}load(){}loadAsync(e,n){let r=this;return new Promise(function(s,o){r.load(e,s,n,o)})}parse(){}setCrossOrigin(e){return this.crossOrigin=e,this}setWithCredentials(e){return this.withCredentials=e,this}setPath(e){return this.path=e,this}setResourcePath(e){return this.resourcePath=e,this}setRequestHeader(e){return this.requestHeader=e,this}abort(){return this}};Ns.DEFAULT_MATERIAL_NAME="__DEFAULT";var _s=new WeakMap,oc=class extends Ns{constructor(e){super(e)}load(e,n,r,s){this.path!==void 0&&(e=this.path+e),e=this.manager.resolveURL(e);let o=this,a=wl.get(`image:${e}`);if(a!==void 0){if(a.complete===!0)o.manager.itemStart(e),setTimeout(function(){n&&n(a),o.manager.itemEnd(e)},0);else{let d=_s.get(a);d===void 0&&(d=[],_s.set(a,d)),d.push({onLoad:n,onError:s})}return a}let l=Ms("img");function c(){h(),n&&n(this);let d=_s.get(this)||[];for(let f=0;f<d.length;f++){let p=d[f];p.onLoad&&p.onLoad(this)}_s.delete(this),o.manager.itemEnd(e)}function u(d){h(),s&&s(d),wl.remove(`image:${e}`);let f=_s.get(this)||[];for(let p=0;p<f.length;p++){let g=f[p];g.onError&&g.onError(d)}_s.delete(this),o.manager.itemError(e),o.manager.itemEnd(e)}function h(){l.removeEventListener("load",c,!1),l.removeEventListener("error",u,!1)}return l.addEventListener("load",c,!1),l.addEventListener("error",u,!1),e.slice(0,5)!=="data:"&&this.crossOrigin!==void 0&&(l.crossOrigin=this.crossOrigin),wl.add(`image:${e}`,l),o.manager.itemStart(e),l.src=e,l}};var ko=class extends Ns{constructor(e){super(e)}load(e,n,r,s){let o=new ln,a=new oc(this.manager);return a.setCrossOrigin(this.crossOrigin),a.setPath(this.path),a.load(e,function(l){o.image=l,o.needsUpdate=!0,n!==void 0&&n(o)},r,s),o}},Bo=class extends Kt{constructor(e,n=1){super(),this.isLight=!0,this.type="Light",this.color=new We(e),this.intensity=n}copy(e,n){return super.copy(e,n),this.color.copy(e.color),this.intensity=e.intensity,this}toJSON(e){let n=super.toJSON(e);return n.object.color=this.color.getHex(),n.object.intensity=this.intensity,n}};var lf=new st,Um=new F,Fm=new F,ac=class{constructor(e){this.camera=e,this.intensity=1,this.bias=0,this.biasNode=null,this.normalBias=0,this.radius=1,this.blurSamples=8,this.mapSize=new fe(512,512),this.mapType=mn,this.map=null,this.mapPass=null,this.matrix=new st,this.autoUpdate=!0,this.needsUpdate=!1,this._frustum=new Ps,this._frameExtents=new fe(1,1),this._viewportCount=1,this._viewports=[new vt(0,0,1,1)]}getViewportCount(){return this._viewportCount}getCamera(){return this.camera}getFrustum(){return this._frustum}updateMatrices(e){let n=this.camera;Um.setFromMatrixPosition(e.matrixWorld),n.position.copy(Um),Fm.setFromMatrixPosition(e.target.matrixWorld),n.lookAt(Fm),n.updateMatrixWorld(),this._updateMatrix(n,this.matrix,this._frustum)}_updateMatrix(e,n,r,s){lf.multiplyMatrices(e.projectionMatrix,e.matrixWorldInverse),r.setFromProjectionMatrix(lf,e.coordinateSystem,e.reversedDepth);let o=this._frameExtents,a=s?s.z/o.x:1,l=s?s.w/o.y:1,c=s?s.x/o.x:0,u=s?s.y/o.y:0;e.coordinateSystem===ws||e.reversedDepth?n.set(.5*a,0,0,.5*a+c,0,.5*l,0,.5*l+u,0,0,1,0,0,0,0,1):n.set(.5*a,0,0,.5*a+c,0,.5*l,0,.5*l+u,0,0,.5,.5,0,0,0,1),n.multiply(lf)}getViewport(e){return this._viewports[e]}getFrameExtents(){return this._frameExtents}dispose(){this.map&&this.map.dispose(),this.mapPass&&this.mapPass.dispose()}copy(e){return this.camera=e.camera.clone(),this.intensity=e.intensity,this.bias=e.bias,this.radius=e.radius,this.autoUpdate=e.autoUpdate,this.needsUpdate=e.needsUpdate,this.normalBias=e.normalBias,this.blurSamples=e.blurSamples,this.mapSize.copy(e.mapSize),this.biasNode=e.biasNode,this}clone(){return new this.constructor().copy(this)}toJSON(){let e={};return e.intensity=this.intensity,e.bias=this.bias,e.normalBias=this.normalBias,e.radius=this.radius,e.blurSamples=this.blurSamples,e.mapSize=this.mapSize.toArray(),e.camera=this.camera.toJSON(!1).object,delete e.camera.matrix,e}},yl=new F,xl=new Ut,ci=new F,zo=class extends Kt{constructor(){super(),this.isCamera=!0,this.type="Camera",this.matrixWorldInverse=new st,this.projectionMatrix=new st,this.projectionMatrixInverse=new st,this.coordinateSystem=jn,this._reversedDepth=!1}get reversedDepth(){return this._reversedDepth}copy(e,n){return super.copy(e,n),this.matrixWorldInverse.copy(e.matrixWorldInverse),this.projectionMatrix.copy(e.projectionMatrix),this.projectionMatrixInverse.copy(e.projectionMatrixInverse),this.coordinateSystem=e.coordinateSystem,this}getWorldDirection(e){return super.getWorldDirection(e).negate()}updateMatrixWorld(e){super.updateMatrixWorld(e),this.matrixWorld.decompose(yl,xl,ci),ci.x===1&&ci.y===1&&ci.z===1?this.matrixWorldInverse.copy(this.matrixWorld).invert():this.matrixWorldInverse.compose(yl,xl,ci.set(1,1,1)).invert()}updateWorldMatrix(e,n,r=!1){super.updateWorldMatrix(e,n,r),this.matrixWorld.decompose(yl,xl,ci),ci.x===1&&ci.y===1&&ci.z===1?this.matrixWorldInverse.copy(this.matrixWorld).invert():this.matrixWorldInverse.compose(yl,xl,ci.set(1,1,1)).invert()}clone(){return new this.constructor().copy(this)}},$i=new F,km=new fe,Bm=new fe,$t=class extends zo{constructor(e=50,n=1,r=.1,s=2e3){super(),this.isPerspectiveCamera=!0,this.type="PerspectiveCamera",this.fov=e,this.zoom=1,this.near=r,this.far=s,this.focus=10,this.aspect=n,this.view=null,this.filmGauge=35,this.filmOffset=0,this.updateProjectionMatrix()}copy(e,n){return super.copy(e,n),this.fov=e.fov,this.zoom=e.zoom,this.near=e.near,this.far=e.far,this.focus=e.focus,this.aspect=e.aspect,this.view=e.view===null?null:Object.assign({},e.view),this.filmGauge=e.filmGauge,this.filmOffset=e.filmOffset,this}setFocalLength(e){let n=.5*this.getFilmHeight()/e;this.fov=As*2*Math.atan(n),this.updateProjectionMatrix()}getFocalLength(){let e=Math.tan(vo*.5*this.fov);return .5*this.getFilmHeight()/e}getEffectiveFOV(){return As*2*Math.atan(Math.tan(vo*.5*this.fov)/this.zoom)}getFilmWidth(){return this.filmGauge*Math.min(this.aspect,1)}getFilmHeight(){return this.filmGauge/Math.max(this.aspect,1)}getViewBounds(e,n,r){$i.set(-1,-1,.5).applyMatrix4(this.projectionMatrixInverse),n.set($i.x,$i.y).multiplyScalar(-e/$i.z),$i.set(1,1,.5).applyMatrix4(this.projectionMatrixInverse),r.set($i.x,$i.y).multiplyScalar(-e/$i.z)}getViewSize(e,n){return this.getViewBounds(e,km,Bm),n.subVectors(Bm,km)}setViewOffset(e,n,r,s,o,a){this.aspect=e/n,this.view===null&&(this.view={enabled:!0,fullWidth:1,fullHeight:1,offsetX:0,offsetY:0,width:1,height:1}),this.view.enabled=!0,this.view.fullWidth=e,this.view.fullHeight=n,this.view.offsetX=r,this.view.offsetY=s,this.view.width=o,this.view.height=a,this.updateProjectionMatrix()}clearViewOffset(){this.view!==null&&(this.view.enabled=!1),this.updateProjectionMatrix()}updateProjectionMatrix(){let e=this.near,n=e*Math.tan(vo*.5*this.fov)/this.zoom,r=2*n,s=this.aspect*r,o=-.5*s,a=this.view;if(this.view!==null&&this.view.enabled){let c=a.fullWidth,u=a.fullHeight;o+=a.offsetX*s/c,n-=a.offsetY*r/u,s*=a.width/c,r*=a.height/u}let l=this.filmOffset;l!==0&&(o+=e*l/this.getFilmWidth()),this.projectionMatrix.makePerspective(o,o+s,n,n-r,e,this.far,this.coordinateSystem,this.reversedDepth),this.projectionMatrixInverse.copy(this.projectionMatrix).invert()}toJSON(e){let n=super.toJSON(e);return n.object.fov=this.fov,n.object.zoom=this.zoom,n.object.near=this.near,n.object.far=this.far,n.object.focus=this.focus,n.object.aspect=this.aspect,this.view!==null&&(n.object.view=Object.assign({},this.view)),n.object.filmGauge=this.filmGauge,n.object.filmOffset=this.filmOffset,n}};var nr=class extends zo{constructor(e=-1,n=1,r=1,s=-1,o=.1,a=2e3){super(),this.isOrthographicCamera=!0,this.type="OrthographicCamera",this.zoom=1,this.view=null,this.left=e,this.right=n,this.top=r,this.bottom=s,this.near=o,this.far=a,this.updateProjectionMatrix()}copy(e,n){return super.copy(e,n),this.left=e.left,this.right=e.right,this.top=e.top,this.bottom=e.bottom,this.near=e.near,this.far=e.far,this.zoom=e.zoom,this.view=e.view===null?null:Object.assign({},e.view),this}setViewOffset(e,n,r,s,o,a){this.view===null&&(this.view={enabled:!0,fullWidth:1,fullHeight:1,offsetX:0,offsetY:0,width:1,height:1}),this.view.enabled=!0,this.view.fullWidth=e,this.view.fullHeight=n,this.view.offsetX=r,this.view.offsetY=s,this.view.width=o,this.view.height=a,this.updateProjectionMatrix()}clearViewOffset(){this.view!==null&&(this.view.enabled=!1),this.updateProjectionMatrix()}updateProjectionMatrix(){let e=(this.right-this.left)/(2*this.zoom),n=(this.top-this.bottom)/(2*this.zoom),r=(this.right+this.left)/2,s=(this.top+this.bottom)/2,o=r-e,a=r+e,l=s+n,c=s-n;if(this.view!==null&&this.view.enabled){let u=(this.right-this.left)/this.view.fullWidth/this.zoom,h=(this.top-this.bottom)/this.view.fullHeight/this.zoom;o+=u*this.view.offsetX,a=o+u*this.view.width,l-=h*this.view.offsetY,c=l-h*this.view.height}this.projectionMatrix.makeOrthographic(o,a,l,c,this.near,this.far,this.coordinateSystem,this.reversedDepth),this.projectionMatrixInverse.copy(this.projectionMatrix).invert()}toJSON(e){let n=super.toJSON(e);return n.object.zoom=this.zoom,n.object.left=this.left,n.object.right=this.right,n.object.top=this.top,n.object.bottom=this.bottom,n.object.near=this.near,n.object.far=this.far,this.view!==null&&(n.object.view=Object.assign({},this.view)),n}},df=class extends ac{constructor(){super(new nr(-5,5,5,-5,.5,500)),this.isDirectionalLightShadow=!0}},Vo=class extends Bo{constructor(e,n){super(e,n),this.isDirectionalLight=!0,this.type="DirectionalLight",this.position.copy(Kt.DEFAULT_UP),this.updateMatrix(),this.target=new Kt,this.shadow=new df}dispose(){super.dispose(),this.shadow.dispose()}copy(e){return super.copy(e),this.target=e.target.clone(),this.shadow=e.shadow.clone(),this}toJSON(e){let n=super.toJSON(e);return n.object.shadow=this.shadow.toJSON(),n.object.target=this.target.uuid,n}},Go=class extends Bo{constructor(e,n){super(e,n),this.isAmbientLight=!0,this.type="AmbientLight"}};var vs=-90,ys=1,lc=class extends Kt{constructor(e,n,r){super(),this.type="CubeCamera",this.renderTarget=r,this.coordinateSystem=null,this.activeMipmapLevel=0;let s=new $t(vs,ys,e,n);s.layers=this.layers,this.add(s);let o=new $t(vs,ys,e,n);o.layers=this.layers,this.add(o);let a=new $t(vs,ys,e,n);a.layers=this.layers,this.add(a);let l=new $t(vs,ys,e,n);l.layers=this.layers,this.add(l);let c=new $t(vs,ys,e,n);c.layers=this.layers,this.add(c);let u=new $t(vs,ys,e,n);u.layers=this.layers,this.add(u)}updateCoordinateSystem(){let e=this.coordinateSystem,n=this.children.concat(),[r,s,o,a,l,c]=n;for(let u of n)this.remove(u);if(e===jn)r.up.set(0,1,0),r.lookAt(1,0,0),s.up.set(0,1,0),s.lookAt(-1,0,0),o.up.set(0,0,-1),o.lookAt(0,1,0),a.up.set(0,0,1),a.lookAt(0,-1,0),l.up.set(0,1,0),l.lookAt(0,0,1),c.up.set(0,1,0),c.lookAt(0,0,-1);else if(e===ws)r.up.set(0,-1,0),r.lookAt(-1,0,0),s.up.set(0,-1,0),s.lookAt(1,0,0),o.up.set(0,0,1),o.lookAt(0,1,0),a.up.set(0,0,-1),a.lookAt(0,-1,0),l.up.set(0,-1,0),l.lookAt(0,0,1),c.up.set(0,-1,0),c.lookAt(0,0,-1);else throw new Error("THREE.CubeCamera.updateCoordinateSystem(): Invalid coordinate system: "+e);for(let u of n)this.add(u),u.updateMatrixWorld()}update(e,n){this.parent===null&&this.updateMatrixWorld();let{renderTarget:r,activeMipmapLevel:s}=this;this.coordinateSystem!==e.coordinateSystem&&(this.coordinateSystem=e.coordinateSystem,this.updateCoordinateSystem());let[o,a,l,c,u,h]=this.children,d=e.getRenderTarget(),f=e.getActiveCubeFace(),p=e.getActiveMipmapLevel(),g=e.xr.enabled;e.xr.enabled=!1;let v=r.texture.generateMipmaps;r.texture.generateMipmaps=!1;let _=!1;e.isWebGLRenderer===!0?_=e.state.buffers.depth.getReversed():_=e.reversedDepthBuffer,e.setRenderTarget(r,0,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,o),e.setRenderTarget(r,1,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,a),e.setRenderTarget(r,2,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,l),e.setRenderTarget(r,3,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,c),e.setRenderTarget(r,4,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,u),r.texture.generateMipmaps=v,e.setRenderTarget(r,5,s),_&&e.autoClear===!1&&e.clearDepth(),e.render(n,h),e.setRenderTarget(d,f,p),e.xr.enabled=g,r.texture.needsPMREMUpdate=!0}},cc=class extends $t{constructor(e=[]){super(),this.isArrayCamera=!0,this.isMultiViewCamera=!1,this.cameras=e}},Or=class{constructor(){this._previousTime=0,this._currentTime=0,this._startTime=performance.now(),this._delta=0,this._elapsed=0,this._timescale=1,this._document=null,this._pageVisibilityHandler=null}connect(e){this._document=e,e.hidden!==void 0&&(this._pageVisibilityHandler=nw.bind(this),e.addEventListener("visibilitychange",this._pageVisibilityHandler,!1))}disconnect(){this._pageVisibilityHandler!==null&&(this._document.removeEventListener("visibilitychange",this._pageVisibilityHandler),this._pageVisibilityHandler=null),this._document=null}getDelta(){return this._delta/1e3}getElapsed(){return this._elapsed/1e3}getTimescale(){return this._timescale}setTimescale(e){return this._timescale=e,this}reset(){return this._currentTime=performance.now()-this._startTime,this}dispose(){this.disconnect()}update(e){return this._pageVisibilityHandler!==null&&this._document.hidden===!0?this._delta=0:(this._previousTime=this._currentTime,this._currentTime=(e!==void 0?e:performance.now())-this._startTime,this._delta=(this._currentTime-this._previousTime)*this._timescale,this._elapsed+=this._delta),this}};function nw(){this._document.hidden===!1&&this.reset()}var Hf="\\[\\]\\.:\\/",iw=new RegExp("["+Hf+"]","g"),Wf="[^"+Hf+"]",rw="[^"+Hf.replace("\\.","")+"]",sw=/((?:WC+[\/:])*)/.source.replace("WC",Wf),ow=/(WCOD+)?/.source.replace("WCOD",rw),aw=/(?:\.(WC+)(?:\[(.+)\])?)?/.source.replace("WC",Wf),lw=/\.(WC+)(?:\[(.+)\])?/.source.replace("WC",Wf),cw=new RegExp("^"+sw+ow+aw+lw+"$"),uw=["material","materials","bones","map"],pf=class{constructor(e,n,r){let s=r||gt.parseTrackName(n);this._targetGroup=e,this._bindings=e.subscribe_(n,s)}getValue(e,n){this.bind();let r=this._targetGroup.nCachedObjects_,s=this._bindings[r];s!==void 0&&s.getValue(e,n)}setValue(e,n){let r=this._bindings;for(let s=this._targetGroup.nCachedObjects_,o=r.length;s!==o;++s)r[s].setValue(e,n)}bind(){let e=this._bindings;for(let n=this._targetGroup.nCachedObjects_,r=e.length;n!==r;++n)e[n].bind()}unbind(){let e=this._bindings;for(let n=this._targetGroup.nCachedObjects_,r=e.length;n!==r;++n)e[n].unbind()}},gt=class i{constructor(e,n,r){this.path=n,this.parsedPath=r||i.parseTrackName(n),this.node=i.findNode(e,this.parsedPath.nodeName),this.rootNode=e,this.getValue=this._getValue_unbound,this.setValue=this._setValue_unbound}static create(e,n,r){return e&&e.isAnimationObjectGroup?new i.Composite(e,n,r):new i(e,n,r)}static sanitizeNodeName(e){return e.replace(/\s/g,"_").replace(iw,"")}static parseTrackName(e){let n=cw.exec(e);if(n===null)throw new Error("THREE.PropertyBinding: Cannot parse trackName: "+e);let r={nodeName:n[2],objectName:n[3],objectIndex:n[4],propertyName:n[5],propertyIndex:n[6]},s=r.nodeName&&r.nodeName.lastIndexOf(".");if(s!==void 0&&s!==-1){let o=r.nodeName.substring(s+1);uw.indexOf(o)!==-1&&(r.nodeName=r.nodeName.substring(0,s),r.objectName=o)}if(r.propertyName===null||r.propertyName.length===0)throw new Error("THREE.PropertyBinding: can not parse propertyName from trackName: "+e);return r}static findNode(e,n){if(n===void 0||n===""||n==="."||n===-1||n===e.name||n===e.uuid)return e;if(e.skeleton){let r=e.skeleton.getBoneByName(n);if(r!==void 0)return r}if(e.children){let r=function(o){for(let a=0;a<o.length;a++){let l=o[a];if(l.name===n||l.uuid===n)return l;let c=r(l.children);if(c)return c}return null},s=r(e.children);if(s)return s}return null}_getValue_unavailable(){}_setValue_unavailable(){}_getValue_direct(e,n){e[n]=this.targetObject[this.propertyName]}_getValue_array(e,n){let r=this.resolvedProperty;for(let s=0,o=r.length;s!==o;++s)e[n++]=r[s]}_getValue_arrayElement(e,n){e[n]=this.resolvedProperty[this.propertyIndex]}_getValue_toArray(e,n){this.resolvedProperty.toArray(e,n)}_setValue_direct(e,n){this.targetObject[this.propertyName]=e[n]}_setValue_direct_setNeedsUpdate(e,n){this.targetObject[this.propertyName]=e[n],this.targetObject.needsUpdate=!0}_setValue_direct_setMatrixWorldNeedsUpdate(e,n){this.targetObject[this.propertyName]=e[n],this.targetObject.matrixWorldNeedsUpdate=!0}_setValue_array(e,n){let r=this.resolvedProperty;for(let s=0,o=r.length;s!==o;++s)r[s]=e[n++]}_setValue_array_setNeedsUpdate(e,n){let r=this.resolvedProperty;for(let s=0,o=r.length;s!==o;++s)r[s]=e[n++];this.targetObject.needsUpdate=!0}_setValue_array_setMatrixWorldNeedsUpdate(e,n){let r=this.resolvedProperty;for(let s=0,o=r.length;s!==o;++s)r[s]=e[n++];this.targetObject.matrixWorldNeedsUpdate=!0}_setValue_arrayElement(e,n){this.resolvedProperty[this.propertyIndex]=e[n]}_setValue_arrayElement_setNeedsUpdate(e,n){this.resolvedProperty[this.propertyIndex]=e[n],this.targetObject.needsUpdate=!0}_setValue_arrayElement_setMatrixWorldNeedsUpdate(e,n){this.resolvedProperty[this.propertyIndex]=e[n],this.targetObject.matrixWorldNeedsUpdate=!0}_setValue_fromArray(e,n){this.resolvedProperty.fromArray(e,n)}_setValue_fromArray_setNeedsUpdate(e,n){this.resolvedProperty.fromArray(e,n),this.targetObject.needsUpdate=!0}_setValue_fromArray_setMatrixWorldNeedsUpdate(e,n){this.resolvedProperty.fromArray(e,n),this.targetObject.matrixWorldNeedsUpdate=!0}_getValue_unbound(e,n){this.bind(),this.getValue(e,n)}_setValue_unbound(e,n){this.bind(),this.setValue(e,n)}bind(){let e=this.node,n=this.parsedPath,r=n.objectName,s=n.propertyName,o=n.propertyIndex;if(e||(e=i.findNode(this.rootNode,n.nodeName),this.node=e),this.getValue=this._getValue_unavailable,this.setValue=this._setValue_unavailable,!e){Ue("PropertyBinding: No target node found for track: "+this.path+".");return}if(r){let u=n.objectIndex;switch(r){case"materials":if(!e.material){ze("PropertyBinding: Can not bind to material as node does not have a material.",this);return}if(!e.material.materials){ze("PropertyBinding: Can not bind to material.materials as node.material does not have a materials array.",this);return}e=e.material.materials;break;case"bones":if(!e.skeleton){ze("PropertyBinding: Can not bind to bones as node does not have a skeleton.",this);return}e=e.skeleton.bones;for(let h=0;h<e.length;h++)if(e[h].name===u){u=h;break}break;case"map":if("map"in e){e=e.map;break}if(!e.material){ze("PropertyBinding: Can not bind to material as node does not have a material.",this);return}if(!e.material.map){ze("PropertyBinding: Can not bind to material.map as node.material does not have a map.",this);return}e=e.material.map;break;default:if(e[r]===void 0){ze("PropertyBinding: Can not bind to objectName of node undefined.",this);return}e=e[r]}if(u!==void 0){if(e[u]===void 0){ze("PropertyBinding: Trying to bind to objectIndex of objectName, but is undefined.",this,e);return}e=e[u]}}let a=e[s];if(a===void 0){let u=n.nodeName;ze("PropertyBinding: Trying to update property for track: "+u+"."+s+" but it wasn't found.",e);return}let l=this.Versioning.None;this.targetObject=e,e.isMaterial===!0?l=this.Versioning.NeedsUpdate:e.isObject3D===!0&&(l=this.Versioning.MatrixWorldNeedsUpdate);let c=this.BindingType.Direct;if(o!==void 0){if(s==="morphTargetInfluences"){if(!e.geometry){ze("PropertyBinding: Can not bind to morphTargetInfluences because node does not have a geometry.",this);return}if(!e.geometry.morphAttributes){ze("PropertyBinding: Can not bind to morphTargetInfluences because node does not have a geometry.morphAttributes.",this);return}e.morphTargetDictionary[o]!==void 0&&(o=e.morphTargetDictionary[o])}c=this.BindingType.ArrayElement,this.resolvedProperty=a,this.propertyIndex=o}else a.fromArray!==void 0&&a.toArray!==void 0?(c=this.BindingType.HasFromToArray,this.resolvedProperty=a):Array.isArray(a)?(c=this.BindingType.EntireArray,this.resolvedProperty=a):this.propertyName=s;this.getValue=this.GetterByBindingType[c],this.setValue=this.SetterByBindingTypeAndVersioning[c][l]}unbind(){this.node=null,this.getValue=this._getValue_unbound,this.setValue=this._setValue_unbound}};gt.Composite=pf;gt.prototype.BindingType={Direct:0,EntireArray:1,ArrayElement:2,HasFromToArray:3};gt.prototype.Versioning={None:0,NeedsUpdate:1,MatrixWorldNeedsUpdate:2};gt.prototype.GetterByBindingType=[gt.prototype._getValue_direct,gt.prototype._getValue_array,gt.prototype._getValue_arrayElement,gt.prototype._getValue_toArray];gt.prototype.SetterByBindingTypeAndVersioning=[[gt.prototype._setValue_direct,gt.prototype._setValue_direct_setNeedsUpdate,gt.prototype._setValue_direct_setMatrixWorldNeedsUpdate],[gt.prototype._setValue_array,gt.prototype._setValue_array_setNeedsUpdate,gt.prototype._setValue_array_setMatrixWorldNeedsUpdate],[gt.prototype._setValue_arrayElement,gt.prototype._setValue_arrayElement_setNeedsUpdate,gt.prototype._setValue_arrayElement_setMatrixWorldNeedsUpdate],[gt.prototype._setValue_fromArray,gt.prototype._setValue_fromArray_setNeedsUpdate,gt.prototype._setValue_fromArray_setMatrixWorldNeedsUpdate]];var ED=new Float32Array(1);var zm=new st,Nr=class{constructor(e,n,r=0,s=1/0){this.ray=new Ki(e,n),this.near=r,this.far=s,this.camera=null,this.layers=new Cs,this.params={Mesh:{},Line:{threshold:1},LOD:{},Points:{threshold:1},Sprite:{}}}set(e,n){this.ray.set(e,n)}setFromCamera(e,n){n.isPerspectiveCamera?(this.ray.origin.setFromMatrixPosition(n.matrixWorld),this.ray.direction.set(e.x,e.y,.5).unproject(n).sub(this.ray.origin).normalize(),this.camera=n):n.isOrthographicCamera?(this.ray.origin.set(e.x,e.y,n.projectionMatrix.elements[14]).unproject(n),this.ray.direction.set(0,0,-1).transformDirection(n.matrixWorld),this.camera=n):ze("Raycaster: Unsupported camera type: "+n.type)}setFromXRController(e){return zm.identity().extractRotation(e.matrixWorld),this.ray.origin.setFromMatrixPosition(e.matrixWorld),this.ray.direction.set(0,0,-1).applyMatrix4(zm),this}intersectObject(e,n=!0,r=[]){return mf(e,this,r,n),r.sort(Vm),r}intersectObjects(e,n=!0,r=[]){for(let s=0,o=e.length;s<o;s++)mf(e[s],this,r,n);return r.sort(Vm),r}};function Vm(i,e){return i.distance-e.distance}function mf(i,e,n,r){let s=!0;if(i.layers.test(e.layers)&&i.raycast(e,n)===!1&&(s=!1),s===!0&&r===!0){let o=i.children;for(let a=0,l=o.length;a<l;a++)mf(o[a],e,n,!0)}}var Us=class{constructor(e=1,n=0,r=0){this.radius=e,this.phi=n,this.theta=r}set(e,n,r){return this.radius=e,this.phi=n,this.theta=r,this}copy(e){return this.radius=e.radius,this.phi=e.phi,this.theta=e.theta,this}makeSafe(){return this.phi=je(this.phi,1e-6,Math.PI-1e-6),this}setFromVector3(e){return this.setFromCartesianCoords(e.x,e.y,e.z)}setFromCartesianCoords(e,n,r){return this.radius=Math.sqrt(e*e+n*n+r*r),this.radius===0?(this.theta=0,this.phi=0):(this.theta=Math.atan2(e,r),this.phi=Math.acos(je(n/this.radius,-1,1))),this}clone(){return new this.constructor().copy(this)}};var Zf=class Zf{constructor(e,n,r,s){this.elements=[1,0,0,1],e!==void 0&&this.set(e,n,r,s)}identity(){return this.set(1,0,0,1),this}fromArray(e,n=0){for(let r=0;r<4;r++)this.elements[r]=e[r+n];return this}set(e,n,r,s){let o=this.elements;return o[0]=e,o[2]=n,o[1]=r,o[3]=s,this}};Zf.prototype.isMatrix2=!0;var gf=Zf;var $n=class extends Yn{constructor(e,n=null){super(),this.object=e,this.domElement=n,this.enabled=!0,this.state=-1,this.keys={},this.mouseButtons={LEFT:null,MIDDLE:null,RIGHT:null},this.touches={ONE:null,TWO:null}}connect(e){this.domElement!==null&&this.disconnect(),this.domElement=e}disconnect(){}dispose(){}update(){}};function Xf(i,e,n,r){let s=hw(r);switch(n){case Nf:return i*e;case Ff:return i*e/s.components*s.byteLength;case vc:return i*e/s.components*s.byteLength;case ar:return i*e*2/s.components*s.byteLength;case yc:return i*e*2/s.components*s.byteLength;case Uf:return i*e*3/s.components*s.byteLength;case Nn:return i*e*4/s.components*s.byteLength;case xc:return i*e*4/s.components*s.byteLength;case qo:case jo:return Math.floor((i+3)/4)*Math.floor((e+3)/4)*8;case Yo:case $o:return Math.floor((i+3)/4)*Math.floor((e+3)/4)*16;case Sc:case Mc:return Math.max(i,16)*Math.max(e,8)/4;case bc:case wc:return Math.max(i,8)*Math.max(e,8)/2;case Ec:case Ac:case Cc:case Rc:return Math.floor((i+3)/4)*Math.floor((e+3)/4)*8;case Tc:case Zo:case Pc:return Math.floor((i+3)/4)*Math.floor((e+3)/4)*16;case Ic:return Math.floor((i+3)/4)*Math.floor((e+3)/4)*16;case Lc:return Math.floor((i+4)/5)*Math.floor((e+3)/4)*16;case Dc:return Math.floor((i+4)/5)*Math.floor((e+4)/5)*16;case Oc:return Math.floor((i+5)/6)*Math.floor((e+4)/5)*16;case Nc:return Math.floor((i+5)/6)*Math.floor((e+5)/6)*16;case Uc:return Math.floor((i+7)/8)*Math.floor((e+4)/5)*16;case Fc:return Math.floor((i+7)/8)*Math.floor((e+5)/6)*16;case kc:return Math.floor((i+7)/8)*Math.floor((e+7)/8)*16;case Bc:return Math.floor((i+9)/10)*Math.floor((e+4)/5)*16;case zc:return Math.floor((i+9)/10)*Math.floor((e+5)/6)*16;case Vc:return Math.floor((i+9)/10)*Math.floor((e+7)/8)*16;case Gc:return Math.floor((i+9)/10)*Math.floor((e+9)/10)*16;case Hc:return Math.floor((i+11)/12)*Math.floor((e+9)/10)*16;case Wc:return Math.floor((i+11)/12)*Math.floor((e+11)/12)*16;case Xc:case qc:case jc:return Math.ceil(i/4)*Math.ceil(e/4)*16;case Yc:case $c:return Math.ceil(i/4)*Math.ceil(e/4)*8;case Ko:case Zc:return Math.ceil(i/4)*Math.ceil(e/4)*16}throw new Error(`Unable to determine texture byte length for ${n} format.`)}function hw(i){switch(i){case mn:case If:return{byteLength:1,components:1};case Bs:case Lf:case En:return{byteLength:2,components:1};case gc:case _c:return{byteLength:2,components:4};case Kn:case mc:case Jn:return{byteLength:4,components:1};case Df:case Of:return{byteLength:4,components:3}}throw new Error(`THREE.TextureUtils: Unknown texture type ${i}.`)}typeof __THREE_DEVTOOLS__<"u"&&__THREE_DEVTOOLS__.dispatchEvent(new CustomEvent("register",{detail:{revision:"186"}}));typeof window<"u"&&(window.__THREE__?Ue("WARNING: Multiple instances of Three.js being imported."):window.__THREE__="186");function Zg(){let i=null,e=!1,n=null,r=null;function s(o,a){r=i.requestAnimationFrame(s),n(o,a)}return{start:function(){e!==!0&&n!==null&&i!==null&&(r=i.requestAnimationFrame(s),e=!0)},stop:function(){i!==null&&i.cancelAnimationFrame(r),e=!1},setAnimationLoop:function(o){n=o},setContext:function(o){i=o}}}function fw(i){let e=new WeakMap;function n(l,c){let u=l.array,h=l.usage,d=u.byteLength,f=i.createBuffer();i.bindBuffer(c,f),i.bufferData(c,u,h),l.onUploadCallback();let p;if(u instanceof Float32Array)p=i.FLOAT;else if(typeof Float16Array<"u"&&u instanceof Float16Array)p=i.HALF_FLOAT;else if(u instanceof Uint16Array)l.isFloat16BufferAttribute?p=i.HALF_FLOAT:p=i.UNSIGNED_SHORT;else if(u instanceof Int16Array)p=i.SHORT;else if(u instanceof Uint32Array)p=i.UNSIGNED_INT;else if(u instanceof Int32Array)p=i.INT;else if(u instanceof Int8Array)p=i.BYTE;else if(u instanceof Uint8Array)p=i.UNSIGNED_BYTE;else if(u instanceof Uint8ClampedArray)p=i.UNSIGNED_BYTE;else throw new Error("THREE.WebGLAttributes: Unsupported buffer data format: "+u);return{buffer:f,type:p,bytesPerElement:u.BYTES_PER_ELEMENT,version:l.version,size:d}}function r(l,c,u){let h=c.array,d=c.updateRanges;if(i.bindBuffer(u,l),d.length===0)i.bufferSubData(u,0,h);else{d.sort((p,g)=>p.start-g.start);let f=0;for(let p=1;p<d.length;p++){let g=d[f],v=d[p];v.start<=g.start+g.count+1?g.count=Math.max(g.count,v.start+v.count-g.start):(++f,d[f]=v)}d.length=f+1;for(let p=0,g=d.length;p<g;p++){let v=d[p];i.bufferSubData(u,v.start*h.BYTES_PER_ELEMENT,h,v.start,v.count)}c.clearUpdateRanges()}c.onUploadCallback()}function s(l){return l.isInterleavedBufferAttribute&&(l=l.data),e.get(l)}function o(l){l.isInterleavedBufferAttribute&&(l=l.data);let c=e.get(l);c&&(i.deleteBuffer(c.buffer),e.delete(l))}function a(l,c){if(l.isInterleavedBufferAttribute&&(l=l.data),l.isGLBufferAttribute){let h=e.get(l);(!h||h.version<l.version)&&e.set(l,{buffer:l.buffer,type:l.type,bytesPerElement:l.elementSize,version:l.version});return}let u=e.get(l);if(u===void 0)e.set(l,n(l,c));else if(u.version<l.version){if(u.size!==l.array.byteLength)throw new Error("THREE.WebGLAttributes: The size of the buffer attribute's array buffer does not match the original size. Resizing buffer attributes is not supported.");r(u.buffer,l,c),u.version=l.version}}return{get:s,remove:o,update:a}}var dw=`#ifdef USE_ALPHAHASH
	if ( diffuseColor.a < getAlphaHashThreshold( vPosition ) ) discard;
#endif`,pw=`#ifdef USE_ALPHAHASH
	const float ALPHA_HASH_SCALE = 0.05;
	float hash2D( vec2 value ) {
		return fract( 1.0e4 * sin( 17.0 * value.x + 0.1 * value.y ) * ( 0.1 + abs( sin( 13.0 * value.y + value.x ) ) ) );
	}
	float hash3D( vec3 value ) {
		return hash2D( vec2( hash2D( value.xy ), value.z ) );
	}
	float getAlphaHashThreshold( vec3 position ) {
		float maxDeriv = max(
			length( dFdx( position.xyz ) ),
			length( dFdy( position.xyz ) )
		);
		float pixScale = 1.0 / ( ALPHA_HASH_SCALE * maxDeriv );
		vec2 pixScales = vec2(
			exp2( floor( log2( pixScale ) ) ),
			exp2( ceil( log2( pixScale ) ) )
		);
		vec2 alpha = vec2(
			hash3D( floor( pixScales.x * position.xyz ) ),
			hash3D( floor( pixScales.y * position.xyz ) )
		);
		float lerpFactor = fract( log2( pixScale ) );
		float x = ( 1.0 - lerpFactor ) * alpha.x + lerpFactor * alpha.y;
		float a = min( lerpFactor, 1.0 - lerpFactor );
		vec3 cases = vec3(
			x * x / ( 2.0 * a * ( 1.0 - a ) ),
			( x - 0.5 * a ) / ( 1.0 - a ),
			1.0 - ( ( 1.0 - x ) * ( 1.0 - x ) / ( 2.0 * a * ( 1.0 - a ) ) )
		);
		float threshold = ( x < ( 1.0 - a ) )
			? ( ( x < a ) ? cases.x : cases.y )
			: cases.z;
		return clamp( threshold , 1.0e-6, 1.0 );
	}
#endif`,mw=`#ifdef USE_ALPHAMAP
	diffuseColor.a *= texture2D( alphaMap, vAlphaMapUv ).g;
#endif`,gw=`#ifdef USE_ALPHAMAP
	uniform sampler2D alphaMap;
#endif`,_w=`#ifdef USE_ALPHATEST
	#ifdef ALPHA_TO_COVERAGE
	diffuseColor.a = smoothstep( alphaTest, alphaTest + fwidth( diffuseColor.a ), diffuseColor.a );
	if ( diffuseColor.a == 0.0 ) discard;
	#else
	if ( diffuseColor.a < alphaTest ) discard;
	#endif
#endif`,vw=`#ifdef USE_ALPHATEST
	uniform float alphaTest;
#endif`,yw=`#ifdef USE_AOMAP
	float ambientOcclusion = ( texture2D( aoMap, vAoMapUv ).r - 1.0 ) * aoMapIntensity + 1.0;
	reflectedLight.indirectDiffuse *= ambientOcclusion;
	#if defined( USE_CLEARCOAT ) 
		clearcoatSpecularIndirect *= ambientOcclusion;
	#endif
	#if defined( USE_SHEEN ) 
		sheenSpecularIndirect *= ambientOcclusion;
	#endif
	#if defined( USE_ENVMAP ) && defined( STANDARD )
		float dotNV = saturate( dot( geometryNormal, geometryViewDir ) );
		reflectedLight.indirectSpecular *= computeSpecularOcclusion( dotNV, ambientOcclusion, material.roughness );
	#endif
#endif`,xw=`#ifdef USE_AOMAP
	uniform sampler2D aoMap;
	uniform float aoMapIntensity;
#endif`,bw=`#ifdef USE_BATCHING
	#if ! defined( GL_ANGLE_multi_draw )
	#define gl_DrawID _gl_DrawID
	uniform int _gl_DrawID;
	#endif
	uniform highp sampler2D batchingTexture;
	uniform highp usampler2D batchingIdTexture;
	mat4 getBatchingMatrix( const in float i ) {
		int size = textureSize( batchingTexture, 0 ).x;
		int j = int( i ) * 4;
		int x = j % size;
		int y = j / size;
		vec4 v1 = texelFetch( batchingTexture, ivec2( x, y ), 0 );
		vec4 v2 = texelFetch( batchingTexture, ivec2( x + 1, y ), 0 );
		vec4 v3 = texelFetch( batchingTexture, ivec2( x + 2, y ), 0 );
		vec4 v4 = texelFetch( batchingTexture, ivec2( x + 3, y ), 0 );
		return mat4( v1, v2, v3, v4 );
	}
	float getIndirectIndex( const in int i ) {
		int size = textureSize( batchingIdTexture, 0 ).x;
		int x = i % size;
		int y = i / size;
		return float( texelFetch( batchingIdTexture, ivec2( x, y ), 0 ).r );
	}
#endif
#ifdef USE_BATCHING_COLOR
	uniform sampler2D batchingColorTexture;
	vec4 getBatchingColor( const in float i ) {
		int size = textureSize( batchingColorTexture, 0 ).x;
		int j = int( i );
		int x = j % size;
		int y = j / size;
		return texelFetch( batchingColorTexture, ivec2( x, y ), 0 );
	}
#endif`,Sw=`#ifdef USE_BATCHING
	mat4 batchingMatrix = getBatchingMatrix( getIndirectIndex( gl_DrawID ) );
#endif`,ww=`vec3 transformed = vec3( position );
#ifdef USE_ALPHAHASH
	vPosition = vec3( position );
#endif`,Mw=`vec3 objectNormal = vec3( normal );
#ifdef USE_TANGENT
	vec3 objectTangent = vec3( tangent.xyz );
#endif`,Ew=`float G_BlinnPhong_Implicit( ) {
	return 0.25;
}
float D_BlinnPhong( const in float shininess, const in float dotNH ) {
	return RECIPROCAL_PI * ( shininess * 0.5 + 1.0 ) * pow( dotNH, shininess );
}
vec3 BRDF_BlinnPhong( const in vec3 lightDir, const in vec3 viewDir, const in vec3 normal, const in vec3 specularColor, const in float shininess ) {
	vec3 halfDir = normalize( lightDir + viewDir );
	float dotNH = saturate( dot( normal, halfDir ) );
	float dotVH = saturate( dot( viewDir, halfDir ) );
	vec3 F = F_Schlick( specularColor, 1.0, dotVH );
	float G = G_BlinnPhong_Implicit( );
	float D = D_BlinnPhong( shininess, dotNH );
	return F * ( G * D );
} // validated`,Aw=`#ifdef USE_IRIDESCENCE
	const mat3 XYZ_TO_REC709 = mat3(
		 3.2404542, -0.9692660,  0.0556434,
		-1.5371385,  1.8760108, -0.2040259,
		-0.4985314,  0.0415560,  1.0572252
	);
	vec3 Fresnel0ToIor( vec3 fresnel0 ) {
		vec3 sqrtF0 = sqrt( fresnel0 );
		return ( vec3( 1.0 ) + sqrtF0 ) / ( vec3( 1.0 ) - sqrtF0 );
	}
	vec3 IorToFresnel0( vec3 transmittedIor, float incidentIor ) {
		return pow2( ( transmittedIor - vec3( incidentIor ) ) / ( transmittedIor + vec3( incidentIor ) ) );
	}
	float IorToFresnel0( float transmittedIor, float incidentIor ) {
		return pow2( ( transmittedIor - incidentIor ) / ( transmittedIor + incidentIor ));
	}
	vec3 evalSensitivity( float OPD, vec3 shift ) {
		float phase = 2.0 * PI * OPD * 1.0e-9;
		vec3 val = vec3( 5.4856e-13, 4.4201e-13, 5.2481e-13 );
		vec3 pos = vec3( 1.6810e+06, 1.7953e+06, 2.2084e+06 );
		vec3 var = vec3( 4.3278e+09, 9.3046e+09, 6.6121e+09 );
		vec3 xyz = val * sqrt( 2.0 * PI * var ) * cos( pos * phase + shift ) * exp( - pow2( phase ) * var );
		xyz.x += 9.7470e-14 * sqrt( 2.0 * PI * 4.5282e+09 ) * cos( 2.2399e+06 * phase + shift[ 0 ] ) * exp( - 4.5282e+09 * pow2( phase ) );
		xyz /= 1.0685e-7;
		vec3 rgb = XYZ_TO_REC709 * xyz;
		return rgb;
	}
	vec3 evalIridescence( float outsideIOR, float eta2, float cosTheta1, float thinFilmThickness, vec3 baseF0 ) {
		vec3 I;
		float iridescenceIOR = mix( outsideIOR, eta2, smoothstep( 0.0, 0.03, thinFilmThickness ) );
		float sinTheta2Sq = pow2( outsideIOR / iridescenceIOR ) * ( 1.0 - pow2( cosTheta1 ) );
		float cosTheta2Sq = 1.0 - sinTheta2Sq;
		if ( cosTheta2Sq < 0.0 ) {
			return vec3( 1.0 );
		}
		float cosTheta2 = sqrt( cosTheta2Sq );
		float R0 = IorToFresnel0( iridescenceIOR, outsideIOR );
		float R12 = F_Schlick( R0, 1.0, cosTheta1 );
		float T121 = 1.0 - R12;
		float phi12 = 0.0;
		if ( iridescenceIOR < outsideIOR ) phi12 = PI;
		float phi21 = PI - phi12;
		vec3 baseIOR = Fresnel0ToIor( clamp( baseF0, 0.0, 0.9999 ) );		vec3 R1 = IorToFresnel0( baseIOR, iridescenceIOR );
		vec3 R23 = F_Schlick( R1, 1.0, cosTheta2 );
		vec3 phi23 = vec3( 0.0 );
		if ( baseIOR[ 0 ] < iridescenceIOR ) phi23[ 0 ] = PI;
		if ( baseIOR[ 1 ] < iridescenceIOR ) phi23[ 1 ] = PI;
		if ( baseIOR[ 2 ] < iridescenceIOR ) phi23[ 2 ] = PI;
		float OPD = 2.0 * iridescenceIOR * thinFilmThickness * cosTheta2;
		vec3 phi = vec3( phi21 ) + phi23;
		vec3 R123 = clamp( R12 * R23, 1e-5, 0.9999 );
		vec3 r123 = sqrt( R123 );
		vec3 Rs = pow2( T121 ) * R23 / ( vec3( 1.0 ) - R123 );
		vec3 C0 = R12 + Rs;
		I = C0;
		vec3 Cm = Rs - T121;
		for ( int m = 1; m <= 2; ++ m ) {
			Cm *= r123;
			vec3 Sm = 2.0 * evalSensitivity( float( m ) * OPD, float( m ) * phi );
			I += Cm * Sm;
		}
		return max( I, vec3( 0.0 ) );
	}
#endif`,Tw=`#ifdef USE_BUMPMAP
	uniform sampler2D bumpMap;
	uniform float bumpScale;
	vec2 dHdxy_fwd() {
		vec2 dSTdx = dFdx( vBumpMapUv );
		vec2 dSTdy = dFdy( vBumpMapUv );
		float Hll = bumpScale * texture2D( bumpMap, vBumpMapUv ).x;
		float dBx = bumpScale * texture2D( bumpMap, vBumpMapUv + dSTdx ).x - Hll;
		float dBy = bumpScale * texture2D( bumpMap, vBumpMapUv + dSTdy ).x - Hll;
		return vec2( dBx, dBy );
	}
	vec3 perturbNormalArb( vec3 surf_pos, vec3 surf_norm, vec2 dHdxy, float faceDirection ) {
		vec3 vSigmaX = normalize( dFdx( surf_pos.xyz ) );
		vec3 vSigmaY = normalize( dFdy( surf_pos.xyz ) );
		vec3 vN = surf_norm;
		vec3 R1 = cross( vSigmaY, vN );
		vec3 R2 = cross( vN, vSigmaX );
		float fDet = dot( vSigmaX, R1 ) * faceDirection;
		vec3 vGrad = sign( fDet ) * ( dHdxy.x * R1 + dHdxy.y * R2 );
		return normalize( abs( fDet ) * surf_norm - vGrad );
	}
#endif`,Cw=`#if NUM_CLIPPING_PLANES > 0
	vec4 plane;
	#ifdef ALPHA_TO_COVERAGE
		float distanceToPlane, distanceGradient;
		float clipOpacity = 1.0;
		#pragma unroll_loop_start
		for ( int i = 0; i < UNION_CLIPPING_PLANES; i ++ ) {
			plane = clippingPlanes[ i ];
			distanceToPlane = - dot( vClipPosition, plane.xyz ) + plane.w;
			distanceGradient = fwidth( distanceToPlane ) / 2.0;
			clipOpacity *= smoothstep( - distanceGradient, distanceGradient, distanceToPlane );
			if ( clipOpacity == 0.0 ) discard;
		}
		#pragma unroll_loop_end
		#if UNION_CLIPPING_PLANES < NUM_CLIPPING_PLANES
			float unionClipOpacity = 1.0;
			#pragma unroll_loop_start
			for ( int i = UNION_CLIPPING_PLANES; i < NUM_CLIPPING_PLANES; i ++ ) {
				plane = clippingPlanes[ i ];
				distanceToPlane = - dot( vClipPosition, plane.xyz ) + plane.w;
				distanceGradient = fwidth( distanceToPlane ) / 2.0;
				unionClipOpacity *= 1.0 - smoothstep( - distanceGradient, distanceGradient, distanceToPlane );
			}
			#pragma unroll_loop_end
			clipOpacity *= 1.0 - unionClipOpacity;
		#endif
		diffuseColor.a *= clipOpacity;
		if ( diffuseColor.a == 0.0 ) discard;
	#else
		#pragma unroll_loop_start
		for ( int i = 0; i < UNION_CLIPPING_PLANES; i ++ ) {
			plane = clippingPlanes[ i ];
			if ( dot( vClipPosition, plane.xyz ) > plane.w ) discard;
		}
		#pragma unroll_loop_end
		#if UNION_CLIPPING_PLANES < NUM_CLIPPING_PLANES
			bool clipped = true;
			#pragma unroll_loop_start
			for ( int i = UNION_CLIPPING_PLANES; i < NUM_CLIPPING_PLANES; i ++ ) {
				plane = clippingPlanes[ i ];
				clipped = ( dot( vClipPosition, plane.xyz ) > plane.w ) && clipped;
			}
			#pragma unroll_loop_end
			if ( clipped ) discard;
		#endif
	#endif
#endif`,Rw=`#if NUM_CLIPPING_PLANES > 0
	varying vec3 vClipPosition;
	uniform vec4 clippingPlanes[ NUM_CLIPPING_PLANES ];
#endif`,Pw=`#if NUM_CLIPPING_PLANES > 0
	varying vec3 vClipPosition;
#endif`,Iw=`#if NUM_CLIPPING_PLANES > 0
	vClipPosition = - mvPosition.xyz;
#endif`,Lw=`#if defined( USE_COLOR ) || defined( USE_COLOR_ALPHA )
	diffuseColor *= vColor;
#endif`,Dw=`#if defined( USE_COLOR ) || defined( USE_COLOR_ALPHA )
	varying vec4 vColor;
#endif`,Ow=`#if defined( USE_COLOR ) || defined( USE_COLOR_ALPHA ) || defined( USE_INSTANCING_COLOR ) || defined( USE_BATCHING_COLOR )
	varying vec4 vColor;
#endif`,Nw=`#if defined( USE_COLOR ) || defined( USE_COLOR_ALPHA ) || defined( USE_INSTANCING_COLOR ) || defined( USE_BATCHING_COLOR )
	vColor = vec4( 1.0 );
#endif
#ifdef USE_COLOR_ALPHA
	vColor *= color;
#elif defined( USE_COLOR )
	vColor.rgb *= color;
#endif
#ifdef USE_INSTANCING_COLOR
	vColor.rgb *= instanceColor.rgb;
#endif
#ifdef USE_BATCHING_COLOR
	vColor *= getBatchingColor( getIndirectIndex( gl_DrawID ) );
#endif`,Uw=`#define PI 3.141592653589793
#define PI2 6.283185307179586
#define PI_HALF 1.5707963267948966
#define RECIPROCAL_PI 0.3183098861837907
#define RECIPROCAL_PI2 0.15915494309189535
#define EPSILON 1e-6
#ifndef saturate
#define saturate( a ) clamp( a, 0.0, 1.0 )
#endif
#define whiteComplement( a ) ( 1.0 - saturate( a ) )
float pow2( const in float x ) { return x*x; }
vec3 pow2( const in vec3 x ) { return x*x; }
float pow3( const in float x ) { return x*x*x; }
float pow4( const in float x ) { float x2 = x*x; return x2*x2; }
float max3( const in vec3 v ) { return max( max( v.x, v.y ), v.z ); }
float average( const in vec3 v ) { return dot( v, vec3( 0.3333333 ) ); }
highp float rand( const in vec2 uv ) {
	const highp float a = 12.9898, b = 78.233, c = 43758.5453;
	highp float dt = dot( uv.xy, vec2( a,b ) ), sn = mod( dt, PI );
	return fract( sin( sn ) * c );
}
#ifdef HIGH_PRECISION
	float precisionSafeLength( vec3 v ) { return length( v ); }
#else
	float precisionSafeLength( vec3 v ) {
		float maxComponent = max3( abs( v ) );
		return length( v / maxComponent ) * maxComponent;
	}
#endif
struct IncidentLight {
	vec3 color;
	vec3 direction;
	bool visible;
};
struct ReflectedLight {
	vec3 directDiffuse;
	vec3 directSpecular;
	vec3 indirectDiffuse;
	vec3 indirectSpecular;
};
#ifdef USE_ALPHAHASH
	varying vec3 vPosition;
#endif
vec3 transformDirection( in vec3 dir, in mat4 matrix ) {
	return normalize( ( matrix * vec4( dir, 0.0 ) ).xyz );
}
#define inverseTransformDirection transformDirectionByInverseViewMatrix
vec3 transformNormalByInverseViewMatrix( in vec3 normal, in mat4 viewMatrix ) {
	return normalize( ( vec4( normal, 0.0 ) * viewMatrix ).xyz );
}
vec3 transformDirectionByInverseViewMatrix( in vec3 dir, in mat4 viewMatrix ) {
	return normalize( ( vec4( dir, 0.0 ) * viewMatrix ).xyz );
}
bool isPerspectiveMatrix( mat4 m ) {
	return m[ 2 ][ 3 ] == - 1.0;
}
vec2 equirectUv( in vec3 dir ) {
	float u = atan( dir.z, dir.x ) * RECIPROCAL_PI2 + 0.5;
	float v = asin( clamp( dir.y, - 1.0, 1.0 ) ) * RECIPROCAL_PI + 0.5;
	return vec2( u, v );
}
vec3 BRDF_Lambert( const in vec3 diffuseColor ) {
	return RECIPROCAL_PI * diffuseColor;
}
vec3 F_Schlick( const in vec3 f0, const in float f90, const in float dotVH ) {
	float fresnel = exp2( ( - 5.55473 * dotVH - 6.98316 ) * dotVH );
	return f0 * ( 1.0 - fresnel ) + ( f90 * fresnel );
}
float F_Schlick( const in float f0, const in float f90, const in float dotVH ) {
	float fresnel = exp2( ( - 5.55473 * dotVH - 6.98316 ) * dotVH );
	return f0 * ( 1.0 - fresnel ) + ( f90 * fresnel );
} // validated`,Fw=`#ifdef ENVMAP_TYPE_CUBE_UV
	#define cubeUV_minMipLevel 4.0
	#define cubeUV_minTileSize 16.0
	float getFace( vec3 direction ) {
		vec3 absDirection = abs( direction );
		float face = - 1.0;
		if ( absDirection.x > absDirection.z ) {
			if ( absDirection.x > absDirection.y )
				face = direction.x > 0.0 ? 0.0 : 3.0;
			else
				face = direction.y > 0.0 ? 1.0 : 4.0;
		} else {
			if ( absDirection.z > absDirection.y )
				face = direction.z > 0.0 ? 2.0 : 5.0;
			else
				face = direction.y > 0.0 ? 1.0 : 4.0;
		}
		return face;
	}
	vec2 getUV( vec3 direction, float face ) {
		vec2 uv;
		if ( face == 0.0 ) {
			uv = vec2( direction.z, direction.y ) / abs( direction.x );
		} else if ( face == 1.0 ) {
			uv = vec2( - direction.x, - direction.z ) / abs( direction.y );
		} else if ( face == 2.0 ) {
			uv = vec2( - direction.x, direction.y ) / abs( direction.z );
		} else if ( face == 3.0 ) {
			uv = vec2( - direction.z, direction.y ) / abs( direction.x );
		} else if ( face == 4.0 ) {
			uv = vec2( - direction.x, direction.z ) / abs( direction.y );
		} else {
			uv = vec2( direction.x, direction.y ) / abs( direction.z );
		}
		return 0.5 * ( uv + 1.0 );
	}
	vec3 bilinearCubeUV( sampler2D envMap, vec3 direction, float mipInt ) {
		float face = getFace( direction );
		float filterInt = max( cubeUV_minMipLevel - mipInt, 0.0 );
		mipInt = max( mipInt, cubeUV_minMipLevel );
		float faceSize = exp2( mipInt );
		highp vec2 uv = getUV( direction, face ) * ( faceSize - 2.0 ) + 1.0;
		if ( face > 2.0 ) {
			uv.y += faceSize;
			face -= 3.0;
		}
		uv.x += face * faceSize;
		uv.x += filterInt * 3.0 * cubeUV_minTileSize;
		uv.y += 4.0 * ( exp2( CUBEUV_MAX_MIP ) - faceSize );
		uv.x *= CUBEUV_TEXEL_WIDTH;
		uv.y *= CUBEUV_TEXEL_HEIGHT;
		#ifdef texture2DGradEXT
			return texture2DGradEXT( envMap, uv, vec2( 0.0 ), vec2( 0.0 ) ).rgb;
		#else
			return texture2D( envMap, uv ).rgb;
		#endif
	}
	#define cubeUV_r0 1.0
	#define cubeUV_m0 - 2.0
	#define cubeUV_r1 0.8
	#define cubeUV_m1 - 1.0
	#define cubeUV_r4 0.4
	#define cubeUV_m4 2.0
	#define cubeUV_r5 0.305
	#define cubeUV_m5 3.0
	#define cubeUV_r6 0.21
	#define cubeUV_m6 4.0
	float roughnessToMip( float roughness ) {
		float mip = 0.0;
		if ( roughness >= cubeUV_r1 ) {
			mip = ( cubeUV_r0 - roughness ) * ( cubeUV_m1 - cubeUV_m0 ) / ( cubeUV_r0 - cubeUV_r1 ) + cubeUV_m0;
		} else if ( roughness >= cubeUV_r4 ) {
			mip = ( cubeUV_r1 - roughness ) * ( cubeUV_m4 - cubeUV_m1 ) / ( cubeUV_r1 - cubeUV_r4 ) + cubeUV_m1;
		} else if ( roughness >= cubeUV_r5 ) {
			mip = ( cubeUV_r4 - roughness ) * ( cubeUV_m5 - cubeUV_m4 ) / ( cubeUV_r4 - cubeUV_r5 ) + cubeUV_m4;
		} else if ( roughness >= cubeUV_r6 ) {
			mip = ( cubeUV_r5 - roughness ) * ( cubeUV_m6 - cubeUV_m5 ) / ( cubeUV_r5 - cubeUV_r6 ) + cubeUV_m5;
		} else {
			mip = - 2.0 * log2( 1.16 * roughness );		}
		return mip;
	}
	vec4 textureCubeUV( sampler2D envMap, vec3 sampleDir, float roughness ) {
		float mip = clamp( roughnessToMip( roughness ), cubeUV_m0, CUBEUV_MAX_MIP );
		float mipF = fract( mip );
		float mipInt = floor( mip );
		vec3 color0 = bilinearCubeUV( envMap, sampleDir, mipInt );
		if ( mipF == 0.0 ) {
			return vec4( color0, 1.0 );
		} else {
			vec3 color1 = bilinearCubeUV( envMap, sampleDir, mipInt + 1.0 );
			return vec4( mix( color0, color1, mipF ), 1.0 );
		}
	}
#endif`,kw=`vec3 transformedNormal = objectNormal;
#ifdef USE_TANGENT
	vec3 transformedTangent = objectTangent;
#endif
#ifdef USE_BATCHING
	mat3 bm = mat3( batchingMatrix );
	transformedNormal /= vec3( dot( bm[ 0 ], bm[ 0 ] ), dot( bm[ 1 ], bm[ 1 ] ), dot( bm[ 2 ], bm[ 2 ] ) );
	transformedNormal = bm * transformedNormal;
	#ifdef USE_TANGENT
		transformedTangent = bm * transformedTangent;
	#endif
#endif
#ifdef USE_INSTANCING
	mat3 im = mat3( instanceMatrix );
	transformedNormal /= vec3( dot( im[ 0 ], im[ 0 ] ), dot( im[ 1 ], im[ 1 ] ), dot( im[ 2 ], im[ 2 ] ) );
	transformedNormal = im * transformedNormal;
	#ifdef USE_TANGENT
		transformedTangent = im * transformedTangent;
	#endif
#endif
transformedNormal = normalMatrix * transformedNormal;
#ifdef FLIP_SIDED
	transformedNormal = - transformedNormal;
#endif
#ifdef USE_TANGENT
	transformedTangent = ( modelViewMatrix * vec4( transformedTangent, 0.0 ) ).xyz;
#endif`,Bw=`#ifdef USE_DISPLACEMENTMAP
	uniform sampler2D displacementMap;
	uniform float displacementScale;
	uniform float displacementBias;
#endif`,zw=`#ifdef USE_DISPLACEMENTMAP
	transformed += normalize( objectNormal ) * ( texture2D( displacementMap, vDisplacementMapUv ).x * displacementScale + displacementBias );
#endif`,Vw=`#ifdef USE_EMISSIVEMAP
	vec4 emissiveColor = texture2D( emissiveMap, vEmissiveMapUv );
	#ifdef DECODE_VIDEO_TEXTURE_EMISSIVE
		emissiveColor = sRGBTransferEOTF( emissiveColor );
	#endif
	totalEmissiveRadiance *= emissiveColor.rgb;
#endif`,Gw=`#ifdef USE_EMISSIVEMAP
	uniform sampler2D emissiveMap;
#endif`,Hw="gl_FragColor = linearToOutputTexel( gl_FragColor );",Ww=`vec4 LinearTransferOETF( in vec4 value ) {
	return value;
}
vec4 sRGBTransferEOTF( in vec4 value ) {
	return vec4( mix( pow( value.rgb * 0.9478672986 + vec3( 0.0521327014 ), vec3( 2.4 ) ), value.rgb * 0.0773993808, vec3( lessThanEqual( value.rgb, vec3( 0.04045 ) ) ) ), value.a );
}
vec4 sRGBTransferOETF( in vec4 value ) {
	return vec4( mix( pow( value.rgb, vec3( 0.41666 ) ) * 1.055 - vec3( 0.055 ), value.rgb * 12.92, vec3( lessThanEqual( value.rgb, vec3( 0.0031308 ) ) ) ), value.a );
}`,Xw=`#ifdef USE_ENVMAP
	#ifdef ENV_WORLDPOS
		vec3 cameraToFrag;
		if ( isOrthographic ) {
			cameraToFrag = normalize( vec3( - viewMatrix[ 0 ][ 2 ], - viewMatrix[ 1 ][ 2 ], - viewMatrix[ 2 ][ 2 ] ) );
		} else {
			cameraToFrag = normalize( vWorldPosition - cameraPosition );
		}
		vec3 worldNormal = transformNormalByInverseViewMatrix( normal, viewMatrix );
		#ifdef ENVMAP_MODE_REFLECTION
			vec3 reflectVec = reflect( cameraToFrag, worldNormal );
		#else
			vec3 reflectVec = refract( cameraToFrag, worldNormal, refractionRatio );
		#endif
	#else
		vec3 reflectVec = vReflect;
	#endif
	#ifdef ENVMAP_TYPE_CUBE
		vec4 envColor = textureCube( envMap, envMapRotation * reflectVec );
		#ifdef ENVMAP_BLENDING_MULTIPLY
			outgoingLight = mix( outgoingLight, outgoingLight * envColor.xyz, specularStrength * reflectivity );
		#elif defined( ENVMAP_BLENDING_MIX )
			outgoingLight = mix( outgoingLight, envColor.xyz, specularStrength * reflectivity );
		#elif defined( ENVMAP_BLENDING_ADD )
			outgoingLight += envColor.xyz * specularStrength * reflectivity;
		#endif
	#endif
#endif`,qw=`#ifdef USE_ENVMAP
	uniform float envMapIntensity;
	uniform mat3 envMapRotation;
	#ifdef ENVMAP_TYPE_CUBE
		uniform samplerCube envMap;
	#else
		uniform sampler2D envMap;
	#endif
#endif`,jw=`#ifdef USE_ENVMAP
	uniform float reflectivity;
	#if defined( USE_BUMPMAP ) || defined( USE_NORMALMAP ) || defined( PHONG ) || defined( LAMBERT )
		#define ENV_WORLDPOS
	#endif
	#ifdef ENV_WORLDPOS
		varying vec3 vWorldPosition;
		uniform float refractionRatio;
	#else
		varying vec3 vReflect;
	#endif
#endif`,Yw=`#ifdef USE_ENVMAP
	#if defined( USE_BUMPMAP ) || defined( USE_NORMALMAP ) || defined( PHONG ) || defined( LAMBERT )
		#define ENV_WORLDPOS
	#endif
	#ifdef ENV_WORLDPOS
		
		varying vec3 vWorldPosition;
	#else
		varying vec3 vReflect;
		uniform float refractionRatio;
	#endif
#endif`,$w=`#ifdef USE_ENVMAP
	#ifdef ENV_WORLDPOS
		vWorldPosition = worldPosition.xyz;
	#else
		vec3 cameraToVertex;
		if ( isOrthographic ) {
			cameraToVertex = normalize( vec3( - viewMatrix[ 0 ][ 2 ], - viewMatrix[ 1 ][ 2 ], - viewMatrix[ 2 ][ 2 ] ) );
		} else {
			cameraToVertex = normalize( worldPosition.xyz - cameraPosition );
		}
		vec3 worldNormal = transformNormalByInverseViewMatrix( transformedNormal, viewMatrix );
		#ifdef ENVMAP_MODE_REFLECTION
			vReflect = reflect( cameraToVertex, worldNormal );
		#else
			vReflect = refract( cameraToVertex, worldNormal, refractionRatio );
		#endif
	#endif
#endif`,Zw=`#ifdef USE_FOG
	vFogDepth = - mvPosition.z;
#endif`,Kw=`#ifdef USE_FOG
	varying float vFogDepth;
#endif`,Jw=`#ifdef USE_FOG
	#ifdef FOG_EXP2
		float fogFactor = 1.0 - exp( - fogDensity * fogDensity * vFogDepth * vFogDepth );
	#else
		float fogFactor = smoothstep( fogNear, fogFar, vFogDepth );
	#endif
	gl_FragColor.rgb = mix( gl_FragColor.rgb, fogColor, fogFactor );
#endif`,Qw=`#ifdef USE_FOG
	uniform vec3 fogColor;
	varying float vFogDepth;
	#ifdef FOG_EXP2
		uniform float fogDensity;
	#else
		uniform float fogNear;
		uniform float fogFar;
	#endif
#endif`,eM=`#ifdef USE_GRADIENTMAP
	uniform sampler2D gradientMap;
#endif
vec3 getGradientIrradiance( vec3 normal, vec3 lightDirection ) {
	float dotNL = dot( normal, lightDirection );
	vec2 coord = vec2( dotNL * 0.5 + 0.5, 0.0 );
	#ifdef USE_GRADIENTMAP
		return vec3( texture2D( gradientMap, coord ).r );
	#else
		vec2 fw = fwidth( coord ) * 0.5;
		return mix( vec3( 0.7 ), vec3( 1.0 ), smoothstep( 0.7 - fw.x, 0.7 + fw.x, coord.x ) );
	#endif
}`,tM=`#ifdef USE_LIGHTMAP
	uniform sampler2D lightMap;
	uniform float lightMapIntensity;
#endif`,nM=`LambertMaterial material;
material.diffuseColor = diffuseColor.rgb;
material.specularStrength = specularStrength;`,iM=`varying vec3 vViewPosition;
struct LambertMaterial {
	vec3 diffuseColor;
	float specularStrength;
};
void RE_Direct_Lambert( const in IncidentLight directLight, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in LambertMaterial material, inout ReflectedLight reflectedLight ) {
	float dotNL = saturate( dot( geometryNormal, directLight.direction ) );
	vec3 irradiance = dotNL * directLight.color;
	reflectedLight.directDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
}
void RE_IndirectDiffuse_Lambert( const in vec3 irradiance, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in LambertMaterial material, inout ReflectedLight reflectedLight ) {
	reflectedLight.indirectDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
}
#define RE_Direct				RE_Direct_Lambert
#define RE_IndirectDiffuse		RE_IndirectDiffuse_Lambert`,rM=`uniform bool receiveShadow;
uniform vec3 ambientLightColor;
#if defined( USE_LIGHT_PROBES )
	uniform vec3 lightProbe[ 9 ];
#endif
vec3 shGetIrradianceAt( in vec3 normal, in vec3 shCoefficients[ 9 ] ) {
	float x = normal.x, y = normal.y, z = normal.z;
	vec3 result = shCoefficients[ 0 ] * 0.886227;
	result += shCoefficients[ 1 ] * 2.0 * 0.511664 * y;
	result += shCoefficients[ 2 ] * 2.0 * 0.511664 * z;
	result += shCoefficients[ 3 ] * 2.0 * 0.511664 * x;
	result += shCoefficients[ 4 ] * 2.0 * 0.429043 * x * y;
	result += shCoefficients[ 5 ] * 2.0 * 0.429043 * y * z;
	result += shCoefficients[ 6 ] * ( 0.743125 * z * z - 0.247708 );
	result += shCoefficients[ 7 ] * 2.0 * 0.429043 * x * z;
	result += shCoefficients[ 8 ] * 0.429043 * ( x * x - y * y );
	return result;
}
vec3 getLightProbeIrradiance( const in vec3 lightProbe[ 9 ], const in vec3 normal ) {
	vec3 worldNormal = transformNormalByInverseViewMatrix( normal, viewMatrix );
	vec3 irradiance = shGetIrradianceAt( worldNormal, lightProbe );
	return irradiance;
}
vec3 getAmbientLightIrradiance( const in vec3 ambientLightColor ) {
	vec3 irradiance = ambientLightColor;
	return irradiance;
}
float getDistanceAttenuation( const in float lightDistance, const in float cutoffDistance, const in float decayExponent ) {
	float distanceFalloff = 1.0 / max( pow( lightDistance, decayExponent ), 0.01 );
	if ( cutoffDistance > 0.0 ) {
		distanceFalloff *= pow2( saturate( 1.0 - pow4( lightDistance / cutoffDistance ) ) );
	}
	return distanceFalloff;
}
float getSpotAttenuation( const in float coneCosine, const in float penumbraCosine, const in float angleCosine ) {
	return smoothstep( coneCosine, penumbraCosine, angleCosine );
}
#if NUM_SUN_LIGHTS > 0
	struct SunLight {
		vec3 direction;
		vec3 color;
	};
	uniform SunLight sunLights[ NUM_SUN_LIGHTS ];
	void getSunLightInfo( const in SunLight sunLight, out IncidentLight light ) {
		light.color = sunLight.color;
		light.direction = sunLight.direction;
		light.visible = true;
	}
#endif
#if NUM_DIR_LIGHTS > 0
	struct DirectionalLight {
		vec3 direction;
		vec3 color;
	};
	uniform DirectionalLight directionalLights[ NUM_DIR_LIGHTS ];
	void getDirectionalLightInfo( const in DirectionalLight directionalLight, out IncidentLight light ) {
		light.color = directionalLight.color;
		light.direction = directionalLight.direction;
		light.visible = true;
	}
#endif
#if NUM_POINT_LIGHTS > 0
	struct PointLight {
		vec3 position;
		vec3 color;
		float distance;
		float decay;
	};
	uniform PointLight pointLights[ NUM_POINT_LIGHTS ];
	void getPointLightInfo( const in PointLight pointLight, const in vec3 geometryPosition, out IncidentLight light ) {
		vec3 lVector = pointLight.position - geometryPosition;
		light.direction = normalize( lVector );
		float lightDistance = length( lVector );
		light.color = pointLight.color;
		light.color *= getDistanceAttenuation( lightDistance, pointLight.distance, pointLight.decay );
		light.visible = ( light.color != vec3( 0.0 ) );
	}
#endif
#if NUM_SPOT_LIGHTS > 0
	struct SpotLight {
		vec3 position;
		vec3 direction;
		vec3 color;
		float distance;
		float decay;
		float coneCos;
		float penumbraCos;
	};
	uniform SpotLight spotLights[ NUM_SPOT_LIGHTS ];
	void getSpotLightInfo( const in SpotLight spotLight, const in vec3 geometryPosition, out IncidentLight light ) {
		vec3 lVector = spotLight.position - geometryPosition;
		light.direction = normalize( lVector );
		float angleCos = dot( light.direction, spotLight.direction );
		float spotAttenuation = getSpotAttenuation( spotLight.coneCos, spotLight.penumbraCos, angleCos );
		if ( spotAttenuation > 0.0 ) {
			float lightDistance = length( lVector );
			light.color = spotLight.color * spotAttenuation;
			light.color *= getDistanceAttenuation( lightDistance, spotLight.distance, spotLight.decay );
			light.visible = ( light.color != vec3( 0.0 ) );
		} else {
			light.color = vec3( 0.0 );
			light.visible = false;
		}
	}
#endif
#if NUM_RECT_AREA_LIGHTS > 0
	struct RectAreaLight {
		vec3 color;
		vec3 position;
		vec3 halfWidth;
		vec3 halfHeight;
	};
	uniform sampler2D ltc_1;	uniform sampler2D ltc_2;
	uniform RectAreaLight rectAreaLights[ NUM_RECT_AREA_LIGHTS ];
#endif
#if NUM_HEMI_LIGHTS > 0
	struct HemisphereLight {
		vec3 direction;
		vec3 skyColor;
		vec3 groundColor;
	};
	uniform HemisphereLight hemisphereLights[ NUM_HEMI_LIGHTS ];
	vec3 getHemisphereLightIrradiance( const in HemisphereLight hemiLight, const in vec3 normal ) {
		float dotNL = dot( normal, hemiLight.direction );
		float hemiDiffuseWeight = 0.5 * dotNL + 0.5;
		vec3 irradiance = mix( hemiLight.groundColor, hemiLight.skyColor, hemiDiffuseWeight );
		return irradiance;
	}
#endif
#include <lightprobes_pars_fragment>`,sM=`#ifdef USE_ENVMAP
	vec3 getIBLIrradiance( const in vec3 normal ) {
		#ifdef ENVMAP_TYPE_CUBE_UV
			vec3 worldNormal = transformNormalByInverseViewMatrix( normal, viewMatrix );
			vec4 envMapColor = textureCubeUV( envMap, envMapRotation * worldNormal, 1.0 );
			return PI * envMapColor.rgb * envMapIntensity;
		#else
			return vec3( 0.0 );
		#endif
	}
	vec3 getIBLRadiance( const in vec3 viewDir, const in vec3 normal, const in float roughness ) {
		#ifdef ENVMAP_TYPE_CUBE_UV
			vec3 reflectVec = reflect( - viewDir, normal );
			reflectVec = normalize( mix( reflectVec, normal, pow4( roughness ) ) );
			reflectVec = transformDirectionByInverseViewMatrix( reflectVec, viewMatrix );
			vec4 envMapColor = textureCubeUV( envMap, envMapRotation * reflectVec, roughness );
			return envMapColor.rgb * envMapIntensity;
		#else
			return vec3( 0.0 );
		#endif
	}
	#ifdef USE_RETROREFLECTION
		vec3 getIBLRetroRadiance( const in vec3 viewDir, const in vec3 normal, const in float roughness ) {
			#ifdef ENVMAP_TYPE_CUBE_UV
				vec3 retroVec = normalize( mix( viewDir, normal, pow4( roughness ) ) );
				retroVec = transformDirectionByInverseViewMatrix( retroVec, viewMatrix );
				vec4 envMapColor = textureCubeUV( envMap, envMapRotation * retroVec, roughness );
				return envMapColor.rgb * envMapIntensity;
			#else
				return vec3( 0.0 );
			#endif
		}
	#endif
	#ifdef USE_ANISOTROPY
		vec3 getIBLAnisotropyRadiance( const in vec3 viewDir, const in vec3 normal, const in float roughness, const in vec3 bitangent, const in float anisotropy ) {
			#ifdef ENVMAP_TYPE_CUBE_UV
				vec3 bentNormal = cross( bitangent, viewDir );
				bentNormal = normalize( cross( bentNormal, bitangent ) );
				bentNormal = normalize( mix( bentNormal, normal, pow2( pow2( 1.0 - anisotropy * ( 1.0 - roughness ) ) ) ) );
				return getIBLRadiance( viewDir, bentNormal, roughness );
			#else
				return vec3( 0.0 );
			#endif
		}
		#ifdef USE_RETROREFLECTION
			vec3 getIBLAnisotropyRetroRadiance( const in vec3 viewDir, const in vec3 normal, const in float roughness, const in vec3 bitangent, const in float anisotropy ) {
				#ifdef ENVMAP_TYPE_CUBE_UV
					vec3 bentNormal = cross( bitangent, viewDir );
					bentNormal = normalize( cross( bentNormal, bitangent ) );
					bentNormal = normalize( mix( bentNormal, normal, pow2( pow2( 1.0 - anisotropy * ( 1.0 - roughness ) ) ) ) );
					return getIBLRetroRadiance( viewDir, bentNormal, roughness );
				#else
					return vec3( 0.0 );
				#endif
			}
		#endif
	#endif
#endif`,oM=`ToonMaterial material;
material.diffuseColor = diffuseColor.rgb;`,aM=`varying vec3 vViewPosition;
struct ToonMaterial {
	vec3 diffuseColor;
};
void RE_Direct_Toon( const in IncidentLight directLight, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in ToonMaterial material, inout ReflectedLight reflectedLight ) {
	vec3 irradiance = getGradientIrradiance( geometryNormal, directLight.direction ) * directLight.color;
	reflectedLight.directDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
}
void RE_IndirectDiffuse_Toon( const in vec3 irradiance, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in ToonMaterial material, inout ReflectedLight reflectedLight ) {
	reflectedLight.indirectDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
}
#define RE_Direct				RE_Direct_Toon
#define RE_IndirectDiffuse		RE_IndirectDiffuse_Toon`,lM=`BlinnPhongMaterial material;
material.diffuseColor = diffuseColor.rgb;
material.specularColor = specular;
material.specularShininess = shininess;
material.specularStrength = specularStrength;`,cM=`varying vec3 vViewPosition;
struct BlinnPhongMaterial {
	vec3 diffuseColor;
	vec3 specularColor;
	float specularShininess;
	float specularStrength;
};
void RE_Direct_BlinnPhong( const in IncidentLight directLight, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in BlinnPhongMaterial material, inout ReflectedLight reflectedLight ) {
	float dotNL = saturate( dot( geometryNormal, directLight.direction ) );
	vec3 irradiance = dotNL * directLight.color;
	reflectedLight.directDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
	reflectedLight.directSpecular += irradiance * BRDF_BlinnPhong( directLight.direction, geometryViewDir, geometryNormal, material.specularColor, material.specularShininess ) * material.specularStrength;
}
void RE_IndirectDiffuse_BlinnPhong( const in vec3 irradiance, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in BlinnPhongMaterial material, inout ReflectedLight reflectedLight ) {
	reflectedLight.indirectDiffuse += irradiance * BRDF_Lambert( material.diffuseColor );
}
#define RE_Direct				RE_Direct_BlinnPhong
#define RE_IndirectDiffuse		RE_IndirectDiffuse_BlinnPhong`,uM=`PhysicalMaterial material;
material.diffuseColor = diffuseColor.rgb;
material.diffuseContribution = diffuseColor.rgb * ( 1.0 - metalnessFactor );
material.metalness = metalnessFactor;
vec3 dxy = max( abs( dFdx( nonPerturbedNormal ) ), abs( dFdy( nonPerturbedNormal ) ) );
float geometryRoughness = max( max( dxy.x, dxy.y ), dxy.z );
material.roughness = max( roughnessFactor, 0.0525 );material.roughness += geometryRoughness;
material.roughness = min( material.roughness, 1.0 );
#ifdef IOR
	material.ior = ior;
	#ifdef USE_SPECULAR
		float specularIntensityFactor = specularIntensity;
		vec3 specularColorFactor = specularColor;
		#ifdef USE_SPECULAR_COLORMAP
			specularColorFactor *= texture2D( specularColorMap, vSpecularColorMapUv ).rgb;
		#endif
		#ifdef USE_SPECULAR_INTENSITYMAP
			specularIntensityFactor *= texture2D( specularIntensityMap, vSpecularIntensityMapUv ).a;
		#endif
		material.specularF90 = mix( specularIntensityFactor, 1.0, metalnessFactor );
	#else
		float specularIntensityFactor = 1.0;
		vec3 specularColorFactor = vec3( 1.0 );
		material.specularF90 = 1.0;
	#endif
	material.specularColor = min( pow2( ( material.ior - 1.0 ) / ( material.ior + 1.0 ) ) * specularColorFactor, vec3( 1.0 ) ) * specularIntensityFactor;
	material.specularColorBlended = mix( material.specularColor, diffuseColor.rgb, metalnessFactor );
#else
	material.specularColor = vec3( 0.04 );
	material.specularColorBlended = mix( material.specularColor, diffuseColor.rgb, metalnessFactor );
	material.specularF90 = 1.0;
#endif
#ifdef USE_CLEARCOAT
	material.clearcoat = clearcoat;
	material.clearcoatRoughness = clearcoatRoughness;
	material.clearcoatF0 = vec3( 0.04 );
	material.clearcoatF90 = 1.0;
	#ifdef USE_CLEARCOATMAP
		material.clearcoat *= texture2D( clearcoatMap, vClearcoatMapUv ).x;
	#endif
	#ifdef USE_CLEARCOAT_ROUGHNESSMAP
		material.clearcoatRoughness *= texture2D( clearcoatRoughnessMap, vClearcoatRoughnessMapUv ).y;
	#endif
	material.clearcoat = saturate( material.clearcoat );	material.clearcoatRoughness = max( material.clearcoatRoughness, 0.0525 );
	material.clearcoatRoughness += geometryRoughness;
	material.clearcoatRoughness = min( material.clearcoatRoughness, 1.0 );
#endif
#ifdef USE_DISPERSION
	material.dispersion = dispersion;
#endif
#ifdef USE_RETROREFLECTION
	material.retroreflectivity = retroreflectivity;
#endif
#ifdef USE_IRIDESCENCE
	material.iridescence = iridescence;
	material.iridescenceIOR = iridescenceIOR;
	#ifdef USE_IRIDESCENCEMAP
		material.iridescence *= texture2D( iridescenceMap, vIridescenceMapUv ).r;
	#endif
	#ifdef USE_IRIDESCENCE_THICKNESSMAP
		material.iridescenceThickness = (iridescenceThicknessMaximum - iridescenceThicknessMinimum) * texture2D( iridescenceThicknessMap, vIridescenceThicknessMapUv ).g + iridescenceThicknessMinimum;
	#else
		material.iridescenceThickness = iridescenceThicknessMaximum;
	#endif
#endif
#ifdef USE_SHEEN
	material.sheenColor = sheenColor;
	#ifdef USE_SHEEN_COLORMAP
		material.sheenColor *= texture2D( sheenColorMap, vSheenColorMapUv ).rgb;
	#endif
	material.sheenRoughness = clamp( sheenRoughness, 0.0001, 1.0 );
	#ifdef USE_SHEEN_ROUGHNESSMAP
		material.sheenRoughness *= texture2D( sheenRoughnessMap, vSheenRoughnessMapUv ).a;
	#endif
#endif
#ifdef USE_ANISOTROPY
	#ifdef USE_ANISOTROPYMAP
		mat2 anisotropyMat = mat2( anisotropyVector.x, anisotropyVector.y, - anisotropyVector.y, anisotropyVector.x );
		vec3 anisotropyPolar = texture2D( anisotropyMap, vAnisotropyMapUv ).rgb;
		vec2 anisotropyV = anisotropyMat * normalize( 2.0 * anisotropyPolar.rg - vec2( 1.0 ) ) * anisotropyPolar.b;
	#else
		vec2 anisotropyV = anisotropyVector;
	#endif
	material.anisotropy = length( anisotropyV );
	if( material.anisotropy == 0.0 ) {
		anisotropyV = vec2( 1.0, 0.0 );
	} else {
		anisotropyV /= material.anisotropy;
		material.anisotropy = saturate( material.anisotropy );
	}
	material.alphaT = mix( pow2( material.roughness ), 1.0, pow2( material.anisotropy ) );
	material.anisotropyT = tbn[ 0 ] * anisotropyV.x + tbn[ 1 ] * anisotropyV.y;
	material.anisotropyB = tbn[ 1 ] * anisotropyV.x - tbn[ 0 ] * anisotropyV.y;
#endif`,hM=`uniform sampler2D dfgLUT;
struct PhysicalMaterial {
	vec3 diffuseColor;
	vec3 diffuseContribution;
	vec3 specularColor;
	vec3 specularColorBlended;
	float roughness;
	float metalness;
	float specularF90;
	float dispersion;
	vec2 dfg;
	vec3 multiScatteringCompensation;
	#ifdef USE_RETROREFLECTION
		float retroreflectivity;
	#endif
	#ifdef USE_CLEARCOAT
		float clearcoat;
		float clearcoatRoughness;
		vec3 clearcoatF0;
		float clearcoatF90;
	#endif
	#ifdef USE_IRIDESCENCE
		float iridescence;
		float iridescenceIOR;
		float iridescenceThickness;
		vec3 iridescenceFresnel;
		vec3 iridescenceF0Dielectric;
		vec3 iridescenceF0Metallic;
	#endif
	#ifdef USE_SHEEN
		vec3 sheenColor;
		float sheenRoughness;
	#endif
	#ifdef IOR
		float ior;
	#endif
	#ifdef USE_TRANSMISSION
		float transmission;
		float transmissionAlpha;
		float thickness;
		float attenuationDistance;
		vec3 attenuationColor;
	#endif
	#ifdef USE_ANISOTROPY
		float anisotropy;
		float alphaT;
		vec3 anisotropyT;
		vec3 anisotropyB;
	#endif
};
vec3 clearcoatSpecularDirect = vec3( 0.0 );
vec3 clearcoatSpecularIndirect = vec3( 0.0 );
vec3 sheenSpecularDirect = vec3( 0.0 );
vec3 sheenSpecularIndirect = vec3(0.0 );
vec3 Schlick_to_F0( const in vec3 f, const in float f90, const in float dotVH ) {
    float x = clamp( 1.0 - dotVH, 0.0, 1.0 );
    float x2 = x * x;
    float x5 = clamp( x * x2 * x2, 0.0, 0.9999 );
    return ( f - vec3( f90 ) * x5 ) / ( 1.0 - x5 );
}
float V_GGX_SmithCorrelated( const in float alpha, const in float dotNL, const in float dotNV ) {
	float a2 = pow2( alpha );
	float gv = dotNL * sqrt( a2 + ( 1.0 - a2 ) * pow2( dotNV ) );
	float gl = dotNV * sqrt( a2 + ( 1.0 - a2 ) * pow2( dotNL ) );
	return 0.5 / max( gv + gl, EPSILON );
}
float D_GGX( const in float alpha, const in float dotNH ) {
	float a2 = pow2( alpha );
	float denom = pow2( dotNH ) * ( a2 - 1.0 ) + 1.0;
	return RECIPROCAL_PI * a2 / pow2( denom );
}
#ifdef USE_ANISOTROPY
	float V_GGX_SmithCorrelated_Anisotropic( const in float alphaT, const in float alphaB, const in float dotTV, const in float dotBV, const in float dotTL, const in float dotBL, const in float dotNV, const in float dotNL ) {
		float gv = dotNL * length( vec3( alphaT * dotTV, alphaB * dotBV, dotNV ) );
		float gl = dotNV * length( vec3( alphaT * dotTL, alphaB * dotBL, dotNL ) );
		return 0.5 / max( gv + gl, EPSILON );
	}
	float D_GGX_Anisotropic( const in float alphaT, const in float alphaB, const in float dotNH, const in float dotTH, const in float dotBH ) {
		float a2 = alphaT * alphaB;
		highp vec3 v = vec3( alphaB * dotTH, alphaT * dotBH, a2 * dotNH );
		highp float v2 = dot( v, v );
		float w2 = a2 / v2;
		return RECIPROCAL_PI * a2 * pow2 ( w2 );
	}
#endif
#ifdef USE_CLEARCOAT
	vec3 BRDF_GGX_Clearcoat( const in vec3 lightDir, const in vec3 viewDir, const in vec3 normal, const in PhysicalMaterial material) {
		vec3 f0 = material.clearcoatF0;
		float f90 = material.clearcoatF90;
		float roughness = material.clearcoatRoughness;
		float alpha = pow2( roughness );
		vec3 halfDir = normalize( lightDir + viewDir );
		float dotNL = saturate( dot( normal, lightDir ) );
		float dotNV = saturate( dot( normal, viewDir ) );
		float dotNH = saturate( dot( normal, halfDir ) );
		float dotVH = saturate( dot( viewDir, halfDir ) );
		vec3 F = F_Schlick( f0, f90, dotVH );
		float V = V_GGX_SmithCorrelated( alpha, dotNL, dotNV );
		float D = D_GGX( alpha, dotNH );
		return F * ( V * D );
	}
#endif
vec3 BRDF_GGX( const in vec3 lightDir, const in vec3 viewDir, const in vec3 normal, const in PhysicalMaterial material ) {
	vec3 f0 = material.specularColorBlended;
	float f90 = material.specularF90;
	float roughness = material.roughness;
	float alpha = pow2( roughness );
	vec3 halfDir = normalize( lightDir + viewDir );
	float dotNL = saturate( dot( normal, lightDir ) );
	float dotNV = saturate( dot( normal, viewDir ) );
	float dotNH = saturate( dot( normal, halfDir ) );
	float dotVH = saturate( dot( viewDir, halfDir ) );
	vec3 F = F_Schlick( f0, f90, dotVH );
	#ifdef USE_IRIDESCENCE
		F = mix( F, material.iridescenceFresnel, material.iridescence );
	#endif
	#ifdef USE_ANISOTROPY
		float dotTL = dot( material.anisotropyT, lightDir );
		float dotTV = dot( material.anisotropyT, viewDir );
		float dotTH = dot( material.anisotropyT, halfDir );
		float dotBL = dot( material.anisotropyB, lightDir );
		float dotBV = dot( material.anisotropyB, viewDir );
		float dotBH = dot( material.anisotropyB, halfDir );
		float V = V_GGX_SmithCorrelated_Anisotropic( material.alphaT, alpha, dotTV, dotBV, dotTL, dotBL, dotNV, dotNL );
		float D = D_GGX_Anisotropic( material.alphaT, alpha, dotNH, dotTH, dotBH );
	#else
		float V = V_GGX_SmithCorrelated( alpha, dotNL, dotNV );
		float D = D_GGX( alpha, dotNH );
	#endif
	return F * ( V * D );
}
vec2 LTC_Uv( const in vec3 N, const in vec3 V, const in float roughness ) {
	const float LUT_SIZE = 64.0;
	const float LUT_SCALE = ( LUT_SIZE - 1.0 ) / LUT_SIZE;
	const float LUT_BIAS = 0.5 / LUT_SIZE;
	float dotNV = saturate( dot( N, V ) );
	vec2 uv = vec2( roughness, sqrt( 1.0 - dotNV ) );
	uv = uv * LUT_SCALE + LUT_BIAS;
	return uv;
}
float LTC_ClippedSphereFormFactor( const in vec3 f ) {
	float l = length( f );
	return max( ( l * l + f.z ) / ( l + 1.0 ), 0.0 );
}
vec3 LTC_EdgeVectorFormFactor( const in vec3 v1, const in vec3 v2 ) {
	float x = dot( v1, v2 );
	float y = abs( x );
	float a = 0.8543985 + ( 0.4965155 + 0.0145206 * y ) * y;
	float b = 3.4175940 + ( 4.1616724 + y ) * y;
	float v = a / b;
	float theta_sintheta = ( x > 0.0 ) ? v : 0.5 * inversesqrt( max( 1.0 - x * x, 1e-7 ) ) - v;
	return cross( v1, v2 ) * theta_sintheta;
}
vec3 LTC_Evaluate( const in vec3 N, const in vec3 V, const in vec3 P, const in mat3 mInv, const in vec3 rectCoords[ 4 ] ) {
	vec3 v1 = rectCoords[ 1 ] - rectCoords[ 0 ];
	vec3 v2 = rectCoords[ 3 ] - rectCoords[ 0 ];
	vec3 lightNormal = cross( v1, v2 );
	if( dot( lightNormal, P - rectCoords[ 0 ] ) < 0.0 ) return vec3( 0.0 );
	vec3 T1, T2;
	T1 = normalize( V - N * dot( V, N ) );
	T2 = - cross( N, T1 );
	mat3 mat = mInv * transpose( mat3( T1, T2, N ) );
	vec3 coords[ 4 ];
	coords[ 0 ] = mat * ( rectCoords[ 0 ] - P );
	coords[ 1 ] = mat * ( rectCoords[ 1 ] - P );
	coords[ 2 ] = mat * ( rectCoords[ 2 ] - P );
	coords[ 3 ] = mat * ( rectCoords[ 3 ] - P );
	coords[ 0 ] = normalize( coords[ 0 ] );
	coords[ 1 ] = normalize( coords[ 1 ] );
	coords[ 2 ] = normalize( coords[ 2 ] );
	coords[ 3 ] = normalize( coords[ 3 ] );
	vec3 vectorFormFactor = vec3( 0.0 );
	vectorFormFactor += LTC_EdgeVectorFormFactor( coords[ 0 ], coords[ 1 ] );
	vectorFormFactor += LTC_EdgeVectorFormFactor( coords[ 1 ], coords[ 2 ] );
	vectorFormFactor += LTC_EdgeVectorFormFactor( coords[ 2 ], coords[ 3 ] );
	vectorFormFactor += LTC_EdgeVectorFormFactor( coords[ 3 ], coords[ 0 ] );
	float result = LTC_ClippedSphereFormFactor( vectorFormFactor );
	return vec3( result );
}
#if defined( USE_SHEEN )
float D_Charlie( float roughness, float dotNH ) {
	float alpha = pow2( roughness );
	float invAlpha = 1.0 / alpha;
	float cos2h = dotNH * dotNH;
	float sin2h = max( 1.0 - cos2h, 0.0078125 );
	return ( 2.0 + invAlpha ) * pow( sin2h, invAlpha * 0.5 ) / ( 2.0 * PI );
}
float V_Neubelt( float dotNV, float dotNL ) {
	return saturate( 1.0 / ( 4.0 * ( dotNL + dotNV - dotNL * dotNV ) ) );
}
vec3 BRDF_Sheen( const in vec3 lightDir, const in vec3 viewDir, const in vec3 normal, vec3 sheenColor, const in float sheenRoughness ) {
	vec3 halfDir = normalize( lightDir + viewDir );
	float dotNL = saturate( dot( normal, lightDir ) );
	float dotNV = saturate( dot( normal, viewDir ) );
	float dotNH = saturate( dot( normal, halfDir ) );
	float D = D_Charlie( sheenRoughness, dotNH );
	float V = V_Neubelt( dotNV, dotNL );
	return sheenColor * ( D * V );
}
#endif
float IBLSheenBRDF( const in vec3 normal, const in vec3 viewDir, const in float roughness ) {
	float dotNV = saturate( dot( normal, viewDir ) );
	float r2 = roughness * roughness;
	float rInv = 1.0 / ( roughness + 0.1 );
	float a = -1.9362 + 1.0678 * roughness + 0.4573 * r2 - 0.8469 * rInv;
	float b = -0.6014 + 0.5538 * roughness - 0.4670 * r2 - 0.1255 * rInv;
	float DG = exp( a * dotNV + b );
	return saturate( DG );
}
vec3 EnvironmentBRDF( const in vec3 normal, const in vec3 viewDir, const in vec3 specularColor, const in float specularF90, const in float roughness ) {
	float dotNV = saturate( dot( normal, viewDir ) );
	vec2 fab = texture2D( dfgLUT, vec2( roughness, dotNV ) ).rg;
	return specularColor * fab.x + specularF90 * fab.y;
}
#ifdef USE_IRIDESCENCE
void computeMultiscatteringIridescence( const in vec2 fab, const in vec3 specularColor, const in float specularF90, const in float iridescence, const in vec3 iridescenceF0, inout vec3 singleScatter, inout vec3 multiScatter ) {
#else
void computeMultiscattering( const in vec2 fab, const in vec3 specularColor, const in float specularF90, inout vec3 singleScatter, inout vec3 multiScatter ) {
#endif
	#ifdef USE_IRIDESCENCE
		vec3 Fr = mix( specularColor, iridescenceF0, iridescence );
	#else
		vec3 Fr = specularColor;
	#endif
	vec3 FssEss = Fr * fab.x + specularF90 * fab.y;
	float Ess = fab.x + fab.y;
	float Ems = 1.0 - Ess;
	vec3 Favg = Fr + ( 1.0 - Fr ) * 0.047619;	vec3 Fms = FssEss * Favg / ( 1.0 - Ems * Favg );
	singleScatter += FssEss;
	multiScatter += Fms * Ems;
}
#if NUM_RECT_AREA_LIGHTS > 0
	void RE_Direct_RectArea_Physical( const in RectAreaLight rectAreaLight, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in PhysicalMaterial material, inout ReflectedLight reflectedLight ) {
		vec3 normal = geometryNormal;
		vec3 viewDir = geometryViewDir;
		vec3 position = geometryPosition;
		vec3 lightPos = rectAreaLight.position;
		vec3 halfWidth = rectAreaLight.halfWidth;
		vec3 halfHeight = rectAreaLight.halfHeight;
		vec3 lightColor = rectAreaLight.color;
		float roughness = material.roughness;
		vec3 rectCoords[ 4 ];
		rectCoords[ 0 ] = lightPos + halfWidth - halfHeight;		rectCoords[ 1 ] = lightPos - halfWidth - halfHeight;
		rectCoords[ 2 ] = lightPos - halfWidth + halfHeight;
		rectCoords[ 3 ] = lightPos + halfWidth + halfHeight;
		vec2 uv = LTC_Uv( normal, viewDir, roughness );
		vec4 t1 = texture2D( ltc_1, uv );
		vec4 t2 = texture2D( ltc_2, uv );
		mat3 mInv = mat3(
			vec3( t1.x, 0, t1.y ),
			vec3(    0, 1,    0 ),
			vec3( t1.z, 0, t1.w )
		);
		vec3 fresnel = ( material.specularColorBlended * t2.x + ( material.specularF90 - material.specularColorBlended ) * t2.y );
		reflectedLight.directSpecular += lightColor * fresnel * LTC_Evaluate( normal, viewDir, position, mInv, rectCoords );
		reflectedLight.directDiffuse += lightColor * material.diffuseContribution * LTC_Evaluate( normal, viewDir, position, mat3( 1.0 ), rectCoords );
		#ifdef USE_CLEARCOAT
			vec3 Ncc = geometryClearcoatNormal;
			vec2 uvClearcoat = LTC_Uv( Ncc, viewDir, material.clearcoatRoughness );
			vec4 t1Clearcoat = texture2D( ltc_1, uvClearcoat );
			vec4 t2Clearcoat = texture2D( ltc_2, uvClearcoat );
			mat3 mInvClearcoat = mat3(
				vec3( t1Clearcoat.x, 0, t1Clearcoat.y ),
				vec3(             0, 1,             0 ),
				vec3( t1Clearcoat.z, 0, t1Clearcoat.w )
			);
			vec3 fresnelClearcoat = material.clearcoatF0 * t2Clearcoat.x + ( material.clearcoatF90 - material.clearcoatF0 ) * t2Clearcoat.y;
			clearcoatSpecularDirect += lightColor * fresnelClearcoat * LTC_Evaluate( Ncc, viewDir, position, mInvClearcoat, rectCoords );
		#endif
	}
#endif
void RE_Direct_Physical( const in IncidentLight directLight, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in PhysicalMaterial material, inout ReflectedLight reflectedLight ) {
	float dotNL = saturate( dot( geometryNormal, directLight.direction ) );
	vec3 irradiance = dotNL * directLight.color;
	#ifdef USE_CLEARCOAT
		float dotNLcc = saturate( dot( geometryClearcoatNormal, directLight.direction ) );
		vec3 ccIrradiance = dotNLcc * directLight.color;
		clearcoatSpecularDirect += ccIrradiance * BRDF_GGX_Clearcoat( directLight.direction, geometryViewDir, geometryClearcoatNormal, material );
	#endif
	#ifdef USE_SHEEN
 
 		sheenSpecularDirect += irradiance * BRDF_Sheen( directLight.direction, geometryViewDir, geometryNormal, material.sheenColor, material.sheenRoughness );
 
 		float sheenAlbedoV = IBLSheenBRDF( geometryNormal, geometryViewDir, material.sheenRoughness );
 		float sheenAlbedoL = IBLSheenBRDF( geometryNormal, directLight.direction, material.sheenRoughness );
 
 		float sheenEnergyComp = 1.0 - max3( material.sheenColor ) * max( sheenAlbedoV, sheenAlbedoL );
 
 		irradiance *= sheenEnergyComp;
 
 	#endif
	vec3 specularBRDF = BRDF_GGX( directLight.direction, geometryViewDir, geometryNormal, material );
	#ifdef USE_RETROREFLECTION
		vec3 retroViewDir = reflect( - geometryViewDir, geometryNormal );
		vec3 retroSpecularBRDF = BRDF_GGX( directLight.direction, retroViewDir, geometryNormal, material );
		specularBRDF = mix( specularBRDF, retroSpecularBRDF, saturate( material.retroreflectivity ) );
	#endif
	reflectedLight.directSpecular += irradiance * specularBRDF * material.multiScatteringCompensation;
	vec3 halfDir = normalize( directLight.direction + geometryViewDir );
	float dotVH = saturate( dot( geometryViewDir, halfDir ) );
	vec3 F = F_Schlick( material.specularColor, material.specularF90, dotVH );
	#ifdef USE_RETROREFLECTION
		vec3 retroHalfDir = normalize( directLight.direction + retroViewDir );
		float dotRetroVH = saturate( dot( retroViewDir, retroHalfDir ) );
		vec3 retroF = F_Schlick( material.specularColor, material.specularF90, dotRetroVH );
		F = mix( F, retroF, saturate( material.retroreflectivity ) );
	#endif
	reflectedLight.directDiffuse += irradiance * BRDF_Lambert( material.diffuseContribution ) * ( 1.0 - F );
}
void RE_IndirectDiffuse_Physical( const in vec3 irradiance, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in PhysicalMaterial material, inout ReflectedLight reflectedLight ) {
	vec3 singleScattering = vec3( 0.0 );
	vec3 multiScattering = vec3( 0.0 );
	#ifdef USE_IRIDESCENCE
		computeMultiscatteringIridescence( material.dfg, material.specularColor, material.specularF90, material.iridescence, material.iridescenceF0Dielectric, singleScattering, multiScattering );
	#else
		computeMultiscattering( material.dfg, material.specularColor, material.specularF90, singleScattering, multiScattering );
	#endif
	vec3 diffuse = irradiance * BRDF_Lambert( material.diffuseContribution ) * ( 1.0 - singleScattering - multiScattering );
	#ifdef USE_SHEEN
		float sheenAlbedo = IBLSheenBRDF( geometryNormal, geometryViewDir, material.sheenRoughness );
		sheenSpecularIndirect += irradiance * material.sheenColor * sheenAlbedo * RECIPROCAL_PI;
		float sheenEnergyComp = 1.0 - max3( material.sheenColor ) * sheenAlbedo;
		diffuse *= sheenEnergyComp;
	#endif
	reflectedLight.indirectDiffuse += diffuse;
}
void RE_IndirectSpecular_Physical( const in vec3 radiance, const in vec3 irradiance, const in vec3 clearcoatRadiance, const in vec3 geometryPosition, const in vec3 geometryNormal, const in vec3 geometryViewDir, const in vec3 geometryClearcoatNormal, const in PhysicalMaterial material, inout ReflectedLight reflectedLight) {
	#ifdef USE_CLEARCOAT
		clearcoatSpecularIndirect += clearcoatRadiance * EnvironmentBRDF( geometryClearcoatNormal, geometryViewDir, material.clearcoatF0, material.clearcoatF90, material.clearcoatRoughness );
	#endif
	#ifdef USE_SHEEN
		sheenSpecularIndirect += irradiance * material.sheenColor * IBLSheenBRDF( geometryNormal, geometryViewDir, material.sheenRoughness ) * RECIPROCAL_PI;
 	#endif
	vec3 singleScatteringDielectric = vec3( 0.0 );
	vec3 multiScatteringDielectric = vec3( 0.0 );
	vec3 singleScatteringMetallic = vec3( 0.0 );
	vec3 multiScatteringMetallic = vec3( 0.0 );
	#ifdef USE_IRIDESCENCE
		computeMultiscatteringIridescence( material.dfg, material.specularColor, material.specularF90, material.iridescence, material.iridescenceF0Dielectric, singleScatteringDielectric, multiScatteringDielectric );
		computeMultiscatteringIridescence( material.dfg, material.diffuseColor, material.specularF90, material.iridescence, material.iridescenceF0Metallic, singleScatteringMetallic, multiScatteringMetallic );
	#else
		computeMultiscattering( material.dfg, material.specularColor, material.specularF90, singleScatteringDielectric, multiScatteringDielectric );
		computeMultiscattering( material.dfg, material.diffuseColor, material.specularF90, singleScatteringMetallic, multiScatteringMetallic );
	#endif
	vec3 singleScattering = mix( singleScatteringDielectric, singleScatteringMetallic, material.metalness );
	vec3 multiScattering = mix( multiScatteringDielectric, multiScatteringMetallic, material.metalness );
	vec3 totalScatteringDielectric = singleScatteringDielectric + multiScatteringDielectric;
	vec3 diffuse = material.diffuseContribution * ( 1.0 - totalScatteringDielectric );
	vec3 cosineWeightedIrradiance = irradiance * RECIPROCAL_PI;
	vec3 indirectSpecular = radiance * singleScattering;
	indirectSpecular += multiScattering * cosineWeightedIrradiance;
	vec3 indirectDiffuse = diffuse * cosineWeightedIrradiance;
	#ifdef USE_SHEEN
		float sheenAlbedo = IBLSheenBRDF( geometryNormal, geometryViewDir, material.sheenRoughness );
		float sheenEnergyComp = 1.0 - max3( material.sheenColor ) * sheenAlbedo;
		indirectSpecular *= sheenEnergyComp;
		indirectDiffuse *= sheenEnergyComp;
	#endif
	reflectedLight.indirectSpecular += indirectSpecular;
	reflectedLight.indirectDiffuse += indirectDiffuse;
}
#define RE_Direct				RE_Direct_Physical
#define RE_Direct_RectArea		RE_Direct_RectArea_Physical
#define RE_IndirectDiffuse		RE_IndirectDiffuse_Physical
#define RE_IndirectSpecular		RE_IndirectSpecular_Physical
float computeSpecularOcclusion( const in float dotNV, const in float ambientOcclusion, const in float roughness ) {
	return saturate( pow( dotNV + ambientOcclusion, exp2( - 16.0 * roughness - 1.0 ) ) - 1.0 + ambientOcclusion );
}`,fM=`
vec3 geometryPosition = - vViewPosition;
vec3 geometryNormal = normal;
vec3 geometryViewDir = ( isOrthographic ) ? vec3( 0, 0, 1 ) : normalize( vViewPosition );
vec3 geometryClearcoatNormal = vec3( 0.0 );
#ifdef USE_CLEARCOAT
	geometryClearcoatNormal = clearcoatNormal;
#endif
#ifdef USE_IRIDESCENCE
	float dotNVi = saturate( dot( normal, geometryViewDir ) );
	if ( material.iridescenceThickness == 0.0 ) {
		material.iridescence = 0.0;
	} else {
		material.iridescence = saturate( material.iridescence );
	}
	if ( material.iridescence > 0.0 ) {
		vec3 iridescenceFresnelDielectric = evalIridescence( 1.0, material.iridescenceIOR, dotNVi, material.iridescenceThickness, material.specularColor );
		vec3 iridescenceFresnelMetallic = evalIridescence( 1.0, material.iridescenceIOR, dotNVi, material.iridescenceThickness, material.diffuseColor );
		material.iridescenceFresnel = mix( iridescenceFresnelDielectric, iridescenceFresnelMetallic, material.metalness );
		material.iridescenceF0Dielectric = Schlick_to_F0( iridescenceFresnelDielectric, 1.0, dotNVi );
		material.iridescenceF0Metallic = Schlick_to_F0( iridescenceFresnelMetallic, 1.0, dotNVi );
	}
#endif
#ifdef STANDARD
	float dotNVms = saturate( dot( geometryNormal, geometryViewDir ) );
	material.dfg = texture2D( dfgLUT, vec2( material.roughness, dotNVms ) ).rg;
	#if ( NUM_SUN_LIGHTS > 0 || NUM_DIR_LIGHTS > 0 || NUM_POINT_LIGHTS > 0 || NUM_SPOT_LIGHTS > 0 )
		float EssMs = material.dfg.x + material.dfg.y;
		material.multiScatteringCompensation = 1.0 + material.specularColorBlended * ( 1.0 / EssMs - 1.0 );
	#endif
#endif
IncidentLight directLight;
#if ( NUM_POINT_LIGHTS > 0 ) && defined( RE_Direct )
	PointLight pointLight;
	#if defined( USE_SHADOWMAP ) && NUM_POINT_LIGHT_SHADOWS > 0
	PointLightShadow pointLightShadow;
	#endif
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_POINT_LIGHTS; i ++ ) {
		pointLight = pointLights[ i ];
		getPointLightInfo( pointLight, geometryPosition, directLight );
		#if defined( USE_SHADOWMAP ) && ( UNROLLED_LOOP_INDEX < NUM_POINT_LIGHT_SHADOWS ) && ( defined( SHADOWMAP_TYPE_PCF ) || defined( SHADOWMAP_TYPE_BASIC ) )
		pointLightShadow = pointLightShadows[ i ];
		directLight.color *= ( directLight.visible && receiveShadow ) ? getPointShadow( pointShadowMap[ i ], pointLightShadow.shadowMapSize, pointLightShadow.shadowIntensity, pointLightShadow.shadowBias, pointLightShadow.shadowRadius, vPointShadowCoord[ i ], pointLightShadow.shadowCameraNear, pointLightShadow.shadowCameraFar ) : 1.0;
		#endif
		RE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
	}
	#pragma unroll_loop_end
#endif
#if ( NUM_SPOT_LIGHTS > 0 ) && defined( RE_Direct )
	SpotLight spotLight;
	vec4 spotColor;
	vec3 spotLightCoord;
	bool inSpotLightMap;
	#if defined( USE_SHADOWMAP ) && NUM_SPOT_LIGHT_SHADOWS > 0
	SpotLightShadow spotLightShadow;
	#endif
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_SPOT_LIGHTS; i ++ ) {
		spotLight = spotLights[ i ];
		getSpotLightInfo( spotLight, geometryPosition, directLight );
		#if ( UNROLLED_LOOP_INDEX < NUM_SPOT_LIGHT_SHADOWS_WITH_MAPS )
		#define SPOT_LIGHT_MAP_INDEX UNROLLED_LOOP_INDEX
		#elif ( UNROLLED_LOOP_INDEX < NUM_SPOT_LIGHT_SHADOWS )
		#define SPOT_LIGHT_MAP_INDEX NUM_SPOT_LIGHT_MAPS
		#else
		#define SPOT_LIGHT_MAP_INDEX ( UNROLLED_LOOP_INDEX - NUM_SPOT_LIGHT_SHADOWS + NUM_SPOT_LIGHT_SHADOWS_WITH_MAPS )
		#endif
		#if ( SPOT_LIGHT_MAP_INDEX < NUM_SPOT_LIGHT_MAPS )
			spotLightCoord = vSpotLightCoord[ i ].xyz / vSpotLightCoord[ i ].w;
			inSpotLightMap = all( lessThan( abs( spotLightCoord * 2. - 1. ), vec3( 1.0 ) ) );
			spotColor = texture2D( spotLightMap[ SPOT_LIGHT_MAP_INDEX ], spotLightCoord.xy );
			directLight.color = inSpotLightMap ? directLight.color * spotColor.rgb : directLight.color;
		#endif
		#undef SPOT_LIGHT_MAP_INDEX
		#if defined( USE_SHADOWMAP ) && ( UNROLLED_LOOP_INDEX < NUM_SPOT_LIGHT_SHADOWS )
		spotLightShadow = spotLightShadows[ i ];
		directLight.color *= ( directLight.visible && receiveShadow ) ? getShadow( spotShadowMap[ i ], spotLightShadow.shadowMapSize, spotLightShadow.shadowIntensity, spotLightShadow.shadowBias, spotLightShadow.shadowRadius, vSpotLightCoord[ i ] ) : 1.0;
		#endif
		RE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
	}
	#pragma unroll_loop_end
#endif
#if ( NUM_SUN_LIGHTS > 0 ) && defined( RE_Direct )
	SunLight sunLight;
	#if defined( USE_SHADOWMAP ) && NUM_SUN_LIGHT_SHADOWS > 0
	SunLightShadow sunLightShadow;
	#endif
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_SUN_LIGHTS; i ++ ) {
		sunLight = sunLights[ i ];
		getSunLightInfo( sunLight, directLight );
		#if defined( USE_SHADOWMAP ) && ( UNROLLED_LOOP_INDEX < NUM_SUN_LIGHT_SHADOWS )
		sunLightShadow = sunLightShadows[ i ];
		directLight.color *= ( directLight.visible && receiveShadow ) ? getSunShadow( sunShadowMap[ i ], sunLightShadow, UNROLLED_LOOP_INDEX ) : 1.0;
		#endif
		RE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
	}
	#pragma unroll_loop_end
#endif
#if ( NUM_DIR_LIGHTS > 0 ) && defined( RE_Direct )
	DirectionalLight directionalLight;
	#if defined( USE_SHADOWMAP ) && NUM_DIR_LIGHT_SHADOWS > 0
	DirectionalLightShadow directionalLightShadow;
	#endif
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_DIR_LIGHTS; i ++ ) {
		directionalLight = directionalLights[ i ];
		getDirectionalLightInfo( directionalLight, directLight );
		#if defined( USE_SHADOWMAP ) && ( UNROLLED_LOOP_INDEX < NUM_DIR_LIGHT_SHADOWS )
		directionalLightShadow = directionalLightShadows[ i ];
		directLight.color *= ( directLight.visible && receiveShadow ) ? getShadow( directionalShadowMap[ i ], directionalLightShadow.shadowMapSize, directionalLightShadow.shadowIntensity, directionalLightShadow.shadowBias, directionalLightShadow.shadowRadius, vDirectionalShadowCoord[ i ] ) : 1.0;
		#endif
		RE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
	}
	#pragma unroll_loop_end
#endif
#if ( NUM_RECT_AREA_LIGHTS > 0 ) && defined( RE_Direct_RectArea )
	RectAreaLight rectAreaLight;
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_RECT_AREA_LIGHTS; i ++ ) {
		rectAreaLight = rectAreaLights[ i ];
		RE_Direct_RectArea( rectAreaLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
	}
	#pragma unroll_loop_end
#endif
#if defined( RE_IndirectDiffuse )
	vec3 iblIrradiance = vec3( 0.0 );
	vec3 irradiance = getAmbientLightIrradiance( ambientLightColor );
	#if defined( USE_LIGHT_PROBES )
		irradiance += getLightProbeIrradiance( lightProbe, geometryNormal );
	#endif
	#if ( NUM_HEMI_LIGHTS > 0 )
		#pragma unroll_loop_start
		for ( int i = 0; i < NUM_HEMI_LIGHTS; i ++ ) {
			irradiance += getHemisphereLightIrradiance( hemisphereLights[ i ], geometryNormal );
		}
		#pragma unroll_loop_end
	#endif
	#ifdef USE_LIGHT_PROBES_GRID
		vec3 probeWorldPos = ( ( vec4( geometryPosition, 1.0 ) - viewMatrix[ 3 ] ) * viewMatrix ).xyz;
		vec3 probeWorldNormal = transformNormalByInverseViewMatrix( geometryNormal, viewMatrix );
		irradiance += getLightProbeGridIrradiance( probeWorldPos, probeWorldNormal );
	#endif
#endif
#if defined( RE_IndirectSpecular )
	vec3 radiance = vec3( 0.0 );
	vec3 clearcoatRadiance = vec3( 0.0 );
#endif`,dM=`#if defined( RE_IndirectDiffuse )
	#ifdef USE_LIGHTMAP
		vec4 lightMapTexel = texture2D( lightMap, vLightMapUv );
		vec3 lightMapIrradiance = lightMapTexel.rgb * lightMapIntensity;
		irradiance += lightMapIrradiance;
	#endif
	#if defined( USE_ENVMAP ) && defined( ENVMAP_TYPE_CUBE_UV )
		#if defined( STANDARD ) || defined( LAMBERT ) || defined( PHONG )
			iblIrradiance += getIBLIrradiance( geometryNormal );
		#endif
	#endif
#endif
#if defined( USE_ENVMAP ) && defined( RE_IndirectSpecular )
	#ifdef USE_ANISOTROPY
		vec3 iblRadiance = getIBLAnisotropyRadiance( geometryViewDir, geometryNormal, material.roughness, material.anisotropyB, material.anisotropy );
	#else
		vec3 iblRadiance = getIBLRadiance( geometryViewDir, geometryNormal, material.roughness );
	#endif
	#ifdef USE_RETROREFLECTION
		#ifdef USE_ANISOTROPY
			vec3 retroIBLRadiance = getIBLAnisotropyRetroRadiance( geometryViewDir, geometryNormal, material.roughness, material.anisotropyB, material.anisotropy );
		#else
			vec3 retroIBLRadiance = getIBLRetroRadiance( geometryViewDir, geometryNormal, material.roughness );
		#endif
		iblRadiance = mix( iblRadiance, retroIBLRadiance, saturate( material.retroreflectivity ) );
	#endif
	radiance += iblRadiance;
	#ifdef USE_CLEARCOAT
		clearcoatRadiance += getIBLRadiance( geometryViewDir, geometryClearcoatNormal, material.clearcoatRoughness );
	#endif
#endif`,pM=`#if defined( RE_IndirectDiffuse )
	#if defined( LAMBERT ) || defined( PHONG )
		irradiance += iblIrradiance;
	#endif
	RE_IndirectDiffuse( irradiance, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
#endif
#if defined( RE_IndirectSpecular )
	RE_IndirectSpecular( radiance, iblIrradiance, clearcoatRadiance, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
#endif`,mM=`#ifdef USE_LIGHT_PROBES_GRID
uniform highp sampler3D probesSH;
uniform vec3 probesMin;
uniform vec3 probesMax;
uniform vec3 probesResolution;
vec3 getLightProbeGridIrradiance( vec3 worldPos, vec3 worldNormal ) {
	vec3 res = probesResolution;
	vec3 gridRange = probesMax - probesMin;
	vec3 resMinusOne = res - 1.0;
	vec3 probeSpacing = gridRange / resMinusOne;
	vec3 samplePos = worldPos + worldNormal * probeSpacing * 0.5;
	vec3 uvw = clamp( ( samplePos - probesMin ) / gridRange, 0.0, 1.0 );
	uvw = uvw * resMinusOne / res + 0.5 / res;
	float nz          = res.z;
	float paddedSlices = nz + 2.0;
	float atlasDepth  = 7.0 * paddedSlices;
	float uvZBase     = uvw.z * nz + 1.0;
	vec4 s0 = texture( probesSH, vec3( uvw.xy, ( uvZBase                       ) / atlasDepth ) );
	vec4 s1 = texture( probesSH, vec3( uvw.xy, ( uvZBase +       paddedSlices   ) / atlasDepth ) );
	vec4 s2 = texture( probesSH, vec3( uvw.xy, ( uvZBase + 2.0 * paddedSlices   ) / atlasDepth ) );
	vec4 s3 = texture( probesSH, vec3( uvw.xy, ( uvZBase + 3.0 * paddedSlices   ) / atlasDepth ) );
	vec4 s4 = texture( probesSH, vec3( uvw.xy, ( uvZBase + 4.0 * paddedSlices   ) / atlasDepth ) );
	vec4 s5 = texture( probesSH, vec3( uvw.xy, ( uvZBase + 5.0 * paddedSlices   ) / atlasDepth ) );
	vec4 s6 = texture( probesSH, vec3( uvw.xy, ( uvZBase + 6.0 * paddedSlices   ) / atlasDepth ) );
	vec3 c0 = s0.xyz;
	vec3 c1 = vec3( s0.w, s1.xy );
	vec3 c2 = vec3( s1.zw, s2.x );
	vec3 c3 = s2.yzw;
	vec3 c4 = s3.xyz;
	vec3 c5 = vec3( s3.w, s4.xy );
	vec3 c6 = vec3( s4.zw, s5.x );
	vec3 c7 = s5.yzw;
	vec3 c8 = s6.xyz;
	float x = worldNormal.x, y = worldNormal.y, z = worldNormal.z;
	vec3 result = c0 * 0.886227;
	result += c1 * 2.0 * 0.511664 * y;
	result += c2 * 2.0 * 0.511664 * z;
	result += c3 * 2.0 * 0.511664 * x;
	result += c4 * 2.0 * 0.429043 * x * y;
	result += c5 * 2.0 * 0.429043 * y * z;
	result += c6 * ( 0.743125 * z * z - 0.247708 );
	result += c7 * 2.0 * 0.429043 * x * z;
	result += c8 * 0.429043 * ( x * x - y * y );
	return max( result, vec3( 0.0 ) );
}
#endif`,gM=`#if defined( USE_LOGARITHMIC_DEPTH_BUFFER )
	gl_FragDepth = vIsPerspective == 0.0 ? gl_FragCoord.z : log2( vFragDepth ) * logDepthBufFC * 0.5;
#endif`,_M=`#if defined( USE_LOGARITHMIC_DEPTH_BUFFER )
	uniform float logDepthBufFC;
	varying float vFragDepth;
	varying float vIsPerspective;
#endif`,vM=`#ifdef USE_LOGARITHMIC_DEPTH_BUFFER
	varying float vFragDepth;
	varying float vIsPerspective;
#endif`,yM=`#ifdef USE_LOGARITHMIC_DEPTH_BUFFER
	vFragDepth = 1.0 + gl_Position.w;
	vIsPerspective = float( isPerspectiveMatrix( projectionMatrix ) );
#endif`,xM=`#ifdef USE_MAP
	vec4 sampledDiffuseColor = texture2D( map, vMapUv );
	#ifdef DECODE_VIDEO_TEXTURE
		sampledDiffuseColor = sRGBTransferEOTF( sampledDiffuseColor );
	#endif
	diffuseColor *= sampledDiffuseColor;
#endif`,bM=`#ifdef USE_MAP
	uniform sampler2D map;
#endif`,SM=`#if defined( USE_MAP ) || defined( USE_ALPHAMAP )
	#if defined( USE_POINTS_UV )
		vec2 uv = vUv;
	#else
		vec2 uv = ( uvTransform * vec3( gl_PointCoord.x, 1.0 - gl_PointCoord.y, 1 ) ).xy;
	#endif
#endif
#ifdef USE_MAP
	diffuseColor *= texture2D( map, uv );
#endif
#ifdef USE_ALPHAMAP
	diffuseColor.a *= texture2D( alphaMap, uv ).g;
#endif`,wM=`#if defined( USE_POINTS_UV )
	varying vec2 vUv;
#else
	#if defined( USE_MAP ) || defined( USE_ALPHAMAP )
		uniform mat3 uvTransform;
	#endif
#endif
#ifdef USE_MAP
	uniform sampler2D map;
#endif
#ifdef USE_ALPHAMAP
	uniform sampler2D alphaMap;
#endif`,MM=`float metalnessFactor = metalness;
#ifdef USE_METALNESSMAP
	vec4 texelMetalness = texture2D( metalnessMap, vMetalnessMapUv );
	metalnessFactor *= texelMetalness.b;
#endif`,EM=`#ifdef USE_METALNESSMAP
	uniform sampler2D metalnessMap;
#endif`,AM=`#ifdef USE_INSTANCING_MORPH
	float morphTargetInfluences[ MORPHTARGETS_COUNT ];
	float morphTargetBaseInfluence = texelFetch( morphTexture, ivec2( 0, gl_InstanceID ), 0 ).r;
	for ( int i = 0; i < MORPHTARGETS_COUNT; i ++ ) {
		morphTargetInfluences[i] =  texelFetch( morphTexture, ivec2( i + 1, gl_InstanceID ), 0 ).r;
	}
#endif`,TM=`#if defined( USE_MORPHCOLORS )
	vColor *= morphTargetBaseInfluence;
	for ( int i = 0; i < MORPHTARGETS_COUNT; i ++ ) {
		#if defined( USE_COLOR_ALPHA )
			if ( morphTargetInfluences[ i ] != 0.0 ) vColor += getMorph( gl_VertexID, i, 2 ) * morphTargetInfluences[ i ];
		#elif defined( USE_COLOR )
			if ( morphTargetInfluences[ i ] != 0.0 ) vColor += getMorph( gl_VertexID, i, 2 ).rgb * morphTargetInfluences[ i ];
		#endif
	}
#endif`,CM=`#ifdef USE_MORPHNORMALS
	objectNormal *= morphTargetBaseInfluence;
	for ( int i = 0; i < MORPHTARGETS_COUNT; i ++ ) {
		if ( morphTargetInfluences[ i ] != 0.0 ) objectNormal += getMorph( gl_VertexID, i, 1 ).xyz * morphTargetInfluences[ i ];
	}
#endif`,RM=`#ifdef USE_MORPHTARGETS
	#ifndef USE_INSTANCING_MORPH
		uniform float morphTargetBaseInfluence;
		uniform float morphTargetInfluences[ MORPHTARGETS_COUNT ];
	#endif
	uniform sampler2DArray morphTargetsTexture;
	uniform ivec2 morphTargetsTextureSize;
	vec4 getMorph( const in int vertexIndex, const in int morphTargetIndex, const in int offset ) {
		int texelIndex = vertexIndex * MORPHTARGETS_TEXTURE_STRIDE + offset;
		int y = texelIndex / morphTargetsTextureSize.x;
		int x = texelIndex - y * morphTargetsTextureSize.x;
		ivec3 morphUV = ivec3( x, y, morphTargetIndex );
		return texelFetch( morphTargetsTexture, morphUV, 0 );
	}
#endif`,PM=`#ifdef USE_MORPHTARGETS
	transformed *= morphTargetBaseInfluence;
	for ( int i = 0; i < MORPHTARGETS_COUNT; i ++ ) {
		if ( morphTargetInfluences[ i ] != 0.0 ) transformed += getMorph( gl_VertexID, i, 0 ).xyz * morphTargetInfluences[ i ];
	}
#endif`,IM=`float faceDirection = gl_FrontFacing ? 1.0 : - 1.0;
#ifdef FLAT_SHADED
	vec3 fdx = dFdx( vViewPosition );
	vec3 fdy = dFdy( vViewPosition );
	vec3 normal = normalize( cross( fdx, fdy ) );
#else
	vec3 normal = normalize( vNormal );
	#ifdef DOUBLE_SIDED
		normal *= faceDirection;
	#endif
#endif
#if defined( USE_NORMALMAP_TANGENTSPACE ) || defined( USE_CLEARCOAT_NORMALMAP ) || defined( USE_ANISOTROPY )
	#ifdef USE_TANGENT
		mat3 tbn = mat3( normalize( vTangent ), normalize( vBitangent ), normal );
	#else
		mat3 tbn = getTangentFrame( - vViewPosition, normal,
		#if defined( USE_NORMALMAP )
			vNormalMapUv
		#elif defined( USE_CLEARCOAT_NORMALMAP )
			vClearcoatNormalMapUv
		#else
			vUv
		#endif
		);
	#endif
	#ifdef DOUBLE_SIDED
		tbn[0] *= faceDirection;
		tbn[1] *= faceDirection;
	#endif
#endif
#ifdef USE_CLEARCOAT_NORMALMAP
	#ifdef USE_TANGENT
		mat3 tbn2 = mat3( normalize( vTangent ), normalize( vBitangent ), normal );
	#else
		mat3 tbn2 = getTangentFrame( - vViewPosition, normal, vClearcoatNormalMapUv );
	#endif
	#ifdef DOUBLE_SIDED
		tbn2[0] *= faceDirection;
		tbn2[1] *= faceDirection;
	#endif
#endif
vec3 nonPerturbedNormal = normal;`,LM=`#ifdef USE_NORMALMAP_OBJECTSPACE
	normal = texture2D( normalMap, vNormalMapUv ).xyz * 2.0 - 1.0;
	#ifdef FLIP_SIDED
		normal = - normal;
	#endif
	#ifdef DOUBLE_SIDED
		normal = normal * faceDirection;
	#endif
	normal = normalize( normalMatrix * normal );
#elif defined( USE_NORMALMAP_TANGENTSPACE )
	vec3 mapN = texture2D( normalMap, vNormalMapUv ).xyz * 2.0 - 1.0;
	#if defined( USE_PACKED_NORMALMAP )
		mapN = vec3( mapN.xy, sqrt( saturate( 1.0 - dot( mapN.xy, mapN.xy ) ) ) );
	#endif
	mapN.xy *= normalScale;
	normal = normalize( tbn * mapN );
#elif defined( USE_BUMPMAP )
	normal = perturbNormalArb( - vViewPosition, normal, dHdxy_fwd(), faceDirection );
#endif`,DM=`#ifndef FLAT_SHADED
	varying vec3 vNormal;
	#ifdef USE_TANGENT
		varying vec3 vTangent;
		varying vec3 vBitangent;
	#endif
#endif`,OM=`#ifndef FLAT_SHADED
	varying vec3 vNormal;
	#ifdef USE_TANGENT
		varying vec3 vTangent;
		varying vec3 vBitangent;
	#endif
#endif`,NM=`#ifndef FLAT_SHADED
	vNormal = normalize( transformedNormal );
	#ifdef USE_TANGENT
		vTangent = normalize( transformedTangent );
		vBitangent = normalize( cross( vNormal, vTangent ) * tangent.w );
		#ifdef FLIP_SIDED
			vBitangent = - vBitangent;
		#endif
	#endif
#endif`,UM=`#ifdef USE_NORMALMAP
	uniform sampler2D normalMap;
	uniform vec2 normalScale;
#endif
#ifdef USE_NORMALMAP_OBJECTSPACE
	uniform mat3 normalMatrix;
#endif
#if ! defined ( USE_TANGENT ) && ( defined ( USE_NORMALMAP_TANGENTSPACE ) || defined ( USE_CLEARCOAT_NORMALMAP ) || defined( USE_ANISOTROPY ) )
	mat3 getTangentFrame( vec3 eye_pos, vec3 surf_norm, vec2 uv ) {
		vec3 q0 = dFdx( eye_pos.xyz );
		vec3 q1 = dFdy( eye_pos.xyz );
		vec2 st0 = dFdx( uv.st );
		vec2 st1 = dFdy( uv.st );
		vec3 N = surf_norm;
		vec3 q1perp = cross( q1, N );
		vec3 q0perp = cross( N, q0 );
		vec3 T = q1perp * st0.x + q0perp * st1.x;
		vec3 B = q1perp * st0.y + q0perp * st1.y;
		float det = max( dot( T, T ), dot( B, B ) );
		float scale = ( det == 0.0 ) ? 0.0 : inversesqrt( det );
		return mat3( T * scale, B * scale, N );
	}
#endif`,FM=`#ifdef USE_CLEARCOAT
	vec3 clearcoatNormal = nonPerturbedNormal;
#endif`,kM=`#ifdef USE_CLEARCOAT_NORMALMAP
	vec3 clearcoatMapN = texture2D( clearcoatNormalMap, vClearcoatNormalMapUv ).xyz * 2.0 - 1.0;
	clearcoatMapN.xy *= clearcoatNormalScale;
	clearcoatNormal = normalize( tbn2 * clearcoatMapN );
#endif`,BM=`#ifdef USE_CLEARCOATMAP
	uniform sampler2D clearcoatMap;
#endif
#ifdef USE_CLEARCOAT_NORMALMAP
	uniform sampler2D clearcoatNormalMap;
	uniform vec2 clearcoatNormalScale;
#endif
#ifdef USE_CLEARCOAT_ROUGHNESSMAP
	uniform sampler2D clearcoatRoughnessMap;
#endif`,zM=`#ifdef USE_IRIDESCENCEMAP
	uniform sampler2D iridescenceMap;
#endif
#ifdef USE_IRIDESCENCE_THICKNESSMAP
	uniform sampler2D iridescenceThicknessMap;
#endif`,VM=`#ifdef OPAQUE
diffuseColor.a = 1.0;
#endif
#ifdef USE_TRANSMISSION
diffuseColor.a *= material.transmissionAlpha;
#endif
gl_FragColor = vec4( outgoingLight, diffuseColor.a );`,GM=`vec3 packNormalToRGB( const in vec3 normal ) {
	return normalize( normal ) * 0.5 + 0.5;
}
vec3 unpackRGBToNormal( const in vec3 rgb ) {
	return 2.0 * rgb.xyz - 1.0;
}
const float PackUpscale = 256. / 255.;const float UnpackDownscale = 255. / 256.;const float ShiftRight8 = 1. / 256.;
const float Inv255 = 1. / 255.;
const vec4 PackFactors = vec4( 1.0, 256.0, 256.0 * 256.0, 256.0 * 256.0 * 256.0 );
const vec2 UnpackFactors2 = vec2( UnpackDownscale, 1.0 / PackFactors.g );
const vec3 UnpackFactors3 = vec3( UnpackDownscale / PackFactors.rg, 1.0 / PackFactors.b );
const vec4 UnpackFactors4 = vec4( UnpackDownscale / PackFactors.rgb, 1.0 / PackFactors.a );
vec4 packDepthToRGBA( const in float v ) {
	if( v <= 0.0 )
		return vec4( 0., 0., 0., 0. );
	if( v >= 1.0 )
		return vec4( 1., 1., 1., 1. );
	float vuf;
	float af = modf( v * PackFactors.a, vuf );
	float bf = modf( vuf * ShiftRight8, vuf );
	float gf = modf( vuf * ShiftRight8, vuf );
	return vec4( vuf * Inv255, gf * PackUpscale, bf * PackUpscale, af );
}
vec3 packDepthToRGB( const in float v ) {
	if( v <= 0.0 )
		return vec3( 0., 0., 0. );
	if( v >= 1.0 )
		return vec3( 1., 1., 1. );
	float vuf;
	float bf = modf( v * PackFactors.b, vuf );
	float gf = modf( vuf * ShiftRight8, vuf );
	return vec3( vuf * Inv255, gf * PackUpscale, bf );
}
vec2 packDepthToRG( const in float v ) {
	if( v <= 0.0 )
		return vec2( 0., 0. );
	if( v >= 1.0 )
		return vec2( 1., 1. );
	float vuf;
	float gf = modf( v * 256., vuf );
	return vec2( vuf * Inv255, gf );
}
float unpackRGBAToDepth( const in vec4 v ) {
	return dot( v, UnpackFactors4 );
}
float unpackRGBToDepth( const in vec3 v ) {
	return dot( v, UnpackFactors3 );
}
float unpackRGToDepth( const in vec2 v ) {
	return v.r * UnpackFactors2.r + v.g * UnpackFactors2.g;
}
vec4 pack2HalfToRGBA( const in vec2 v ) {
	vec4 r = vec4( v.x, fract( v.x * 255.0 ), v.y, fract( v.y * 255.0 ) );
	return vec4( r.x - r.y / 255.0, r.y, r.z - r.w / 255.0, r.w );
}
vec2 unpackRGBATo2Half( const in vec4 v ) {
	return vec2( v.x + ( v.y / 255.0 ), v.z + ( v.w / 255.0 ) );
}
float viewZToOrthographicDepth( const in float viewZ, const in float near, const in float far ) {
	return ( viewZ + near ) / ( near - far );
}
float orthographicDepthToViewZ( const in float depth, const in float near, const in float far ) {
	#ifdef USE_REVERSED_DEPTH_BUFFER
	
		return depth * ( far - near ) - far;
	#else
		return depth * ( near - far ) - near;
	#endif
}
float viewZToPerspectiveDepth( const in float viewZ, const in float near, const in float far ) {
	return ( ( near + viewZ ) * far ) / ( ( far - near ) * viewZ );
}
float perspectiveDepthToViewZ( const in float depth, const in float near, const in float far ) {
	
	#ifdef USE_REVERSED_DEPTH_BUFFER
		return ( near * far ) / ( ( near - far ) * depth - near );
	#else
		return ( near * far ) / ( ( far - near ) * depth - far );
	#endif
}`,HM=`#ifdef PREMULTIPLIED_ALPHA
	gl_FragColor.rgb *= gl_FragColor.a;
#endif`,WM=`vec4 mvPosition = vec4( transformed, 1.0 );
#ifdef USE_BATCHING
	mvPosition = batchingMatrix * mvPosition;
#endif
#ifdef USE_INSTANCING
	mvPosition = instanceMatrix * mvPosition;
#endif
mvPosition = modelViewMatrix * mvPosition;
gl_Position = projectionMatrix * mvPosition;`,XM=`#ifdef DITHERING
	gl_FragColor.rgb = dithering( gl_FragColor.rgb );
#endif`,qM=`#ifdef DITHERING
	vec3 dithering( vec3 color ) {
		float grid_position = rand( gl_FragCoord.xy );
		vec3 dither_shift_RGB = vec3( 0.25 / 255.0, -0.25 / 255.0, 0.25 / 255.0 );
		dither_shift_RGB = mix( 2.0 * dither_shift_RGB, -2.0 * dither_shift_RGB, grid_position );
		return color + dither_shift_RGB;
	}
#endif`,jM=`float roughnessFactor = roughness;
#ifdef USE_ROUGHNESSMAP
	vec4 texelRoughness = texture2D( roughnessMap, vRoughnessMapUv );
	roughnessFactor *= texelRoughness.g;
#endif`,YM=`#ifdef USE_ROUGHNESSMAP
	uniform sampler2D roughnessMap;
#endif`,$M=`#if NUM_SPOT_LIGHT_COORDS > 0
	varying vec4 vSpotLightCoord[ NUM_SPOT_LIGHT_COORDS ];
#endif
#if NUM_SPOT_LIGHT_MAPS > 0
	uniform sampler2D spotLightMap[ NUM_SPOT_LIGHT_MAPS ];
#endif
#ifdef USE_SHADOWMAP
	#if NUM_SUN_LIGHT_SHADOWS > 0
		#define SUN_LIGHT_CASCADES 2
		#if defined( SHADOWMAP_TYPE_PCF )
			uniform sampler2DShadow sunShadowMap[ NUM_SUN_LIGHT_SHADOWS ];
		#else
			uniform sampler2D sunShadowMap[ NUM_SUN_LIGHT_SHADOWS ];
		#endif
		uniform mat4 sunShadowMatrix[ NUM_SUN_LIGHT_SHADOWS * SUN_LIGHT_CASCADES ];
		uniform vec4 sunShadowCascade[ NUM_SUN_LIGHT_SHADOWS * SUN_LIGHT_CASCADES ];
		varying vec4 vSunShadowWorldPosition;
		varying vec3 vSunShadowWorldNormal;
		struct SunLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
		};
		uniform SunLightShadow sunLightShadows[ NUM_SUN_LIGHT_SHADOWS ];
	#endif
	#if NUM_DIR_LIGHT_SHADOWS > 0
		#if defined( SHADOWMAP_TYPE_PCF )
			uniform sampler2DShadow directionalShadowMap[ NUM_DIR_LIGHT_SHADOWS ];
		#else
			uniform sampler2D directionalShadowMap[ NUM_DIR_LIGHT_SHADOWS ];
		#endif
		varying vec4 vDirectionalShadowCoord[ NUM_DIR_LIGHT_SHADOWS ];
		struct DirectionalLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
		};
		uniform DirectionalLightShadow directionalLightShadows[ NUM_DIR_LIGHT_SHADOWS ];
	#endif
	#if NUM_SPOT_LIGHT_SHADOWS > 0
		#if defined( SHADOWMAP_TYPE_PCF )
			uniform sampler2DShadow spotShadowMap[ NUM_SPOT_LIGHT_SHADOWS ];
		#else
			uniform sampler2D spotShadowMap[ NUM_SPOT_LIGHT_SHADOWS ];
		#endif
		struct SpotLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
		};
		uniform SpotLightShadow spotLightShadows[ NUM_SPOT_LIGHT_SHADOWS ];
	#endif
	#if NUM_POINT_LIGHT_SHADOWS > 0
		#if defined( SHADOWMAP_TYPE_PCF )
			uniform samplerCubeShadow pointShadowMap[ NUM_POINT_LIGHT_SHADOWS ];
		#elif defined( SHADOWMAP_TYPE_BASIC )
			uniform samplerCube pointShadowMap[ NUM_POINT_LIGHT_SHADOWS ];
		#endif
		varying vec4 vPointShadowCoord[ NUM_POINT_LIGHT_SHADOWS ];
		struct PointLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
			float shadowCameraNear;
			float shadowCameraFar;
		};
		uniform PointLightShadow pointLightShadows[ NUM_POINT_LIGHT_SHADOWS ];
	#endif
	#if defined( SHADOWMAP_TYPE_PCF )
		float interleavedGradientNoise( vec2 position ) {
			return fract( 52.9829189 * fract( dot( position, vec2( 0.06711056, 0.00583715 ) ) ) );
		}
		vec2 vogelDiskSample( int sampleIndex, int samplesCount, float phi ) {
			const float goldenAngle = 2.399963229728653;
			float r = sqrt( ( float( sampleIndex ) + 0.5 ) / float( samplesCount ) );
			float theta = float( sampleIndex ) * goldenAngle + phi;
			return vec2( cos( theta ), sin( theta ) ) * r;
		}
	#endif
	#if defined( SHADOWMAP_TYPE_PCF )
		float getShadow( sampler2DShadow shadowMap, vec2 shadowMapSize, float shadowIntensity, float shadowBias, float shadowRadius, vec4 shadowCoord ) {
			float shadow = 1.0;
			shadowCoord.xyz /= shadowCoord.w;
			shadowCoord.z += shadowBias;
			bool inFrustum = shadowCoord.x >= 0.0 && shadowCoord.x <= 1.0 && shadowCoord.y >= 0.0 && shadowCoord.y <= 1.0;
			bool frustumTest = inFrustum && shadowCoord.z <= 1.0;
			if ( frustumTest ) {
				vec2 texelSize = vec2( 1.0 ) / shadowMapSize;
				float radius = shadowRadius * texelSize.x;
				float phi = interleavedGradientNoise( gl_FragCoord.xy ) * PI2;
				shadow = (
					texture( shadowMap, vec3( shadowCoord.xy + vogelDiskSample( 0, 5, phi ) * radius, shadowCoord.z ) ) +
					texture( shadowMap, vec3( shadowCoord.xy + vogelDiskSample( 1, 5, phi ) * radius, shadowCoord.z ) ) +
					texture( shadowMap, vec3( shadowCoord.xy + vogelDiskSample( 2, 5, phi ) * radius, shadowCoord.z ) ) +
					texture( shadowMap, vec3( shadowCoord.xy + vogelDiskSample( 3, 5, phi ) * radius, shadowCoord.z ) ) +
					texture( shadowMap, vec3( shadowCoord.xy + vogelDiskSample( 4, 5, phi ) * radius, shadowCoord.z ) )
				) * 0.2;
			}
			return mix( 1.0, shadow, shadowIntensity );
		}
	#elif defined( SHADOWMAP_TYPE_VSM )
		float getShadow( sampler2D shadowMap, vec2 shadowMapSize, float shadowIntensity, float shadowBias, float shadowRadius, vec4 shadowCoord ) {
			float shadow = 1.0;
			shadowCoord.xyz /= shadowCoord.w;
			#ifdef USE_REVERSED_DEPTH_BUFFER
				shadowCoord.z -= shadowBias;
			#else
				shadowCoord.z += shadowBias;
			#endif
			bool inFrustum = shadowCoord.x >= 0.0 && shadowCoord.x <= 1.0 && shadowCoord.y >= 0.0 && shadowCoord.y <= 1.0;
			bool frustumTest = inFrustum && shadowCoord.z <= 1.0;
			if ( frustumTest ) {
				vec2 distribution = texture2D( shadowMap, shadowCoord.xy ).rg;
				float mean = distribution.x;
				float variance = distribution.y * distribution.y;
				#ifdef USE_REVERSED_DEPTH_BUFFER
					float hard_shadow = step( mean, shadowCoord.z );
				#else
					float hard_shadow = step( shadowCoord.z, mean );
				#endif
				
				if ( hard_shadow == 1.0 ) {
					shadow = 1.0;
				} else {
					variance = max( variance, 0.0000001 );
					float d = shadowCoord.z - mean;
					float p_max = variance / ( variance + d * d );
					p_max = clamp( ( p_max - 0.3 ) / 0.65, 0.0, 1.0 );
					shadow = max( hard_shadow, p_max );
				}
			}
			return mix( 1.0, shadow, shadowIntensity );
		}
	#else
		float getShadow( sampler2D shadowMap, vec2 shadowMapSize, float shadowIntensity, float shadowBias, float shadowRadius, vec4 shadowCoord ) {
			float shadow = 1.0;
			shadowCoord.xyz /= shadowCoord.w;
			#ifdef USE_REVERSED_DEPTH_BUFFER
				shadowCoord.z -= shadowBias;
			#else
				shadowCoord.z += shadowBias;
			#endif
			bool inFrustum = shadowCoord.x >= 0.0 && shadowCoord.x <= 1.0 && shadowCoord.y >= 0.0 && shadowCoord.y <= 1.0;
			bool frustumTest = inFrustum && shadowCoord.z <= 1.0;
			if ( frustumTest ) {
				float depth = texture2D( shadowMap, shadowCoord.xy ).r;
				#ifdef USE_REVERSED_DEPTH_BUFFER
					shadow = step( depth, shadowCoord.z );
				#else
					shadow = step( shadowCoord.z, depth );
				#endif
			}
			return mix( 1.0, shadow, shadowIntensity );
		}
	#endif
	#if NUM_SUN_LIGHT_SHADOWS > 0
		float getSunShadow(
			#if defined( SHADOWMAP_TYPE_PCF )
				sampler2DShadow shadowMap,
			#else
				sampler2D shadowMap,
			#endif
			SunLightShadow sunLightShadow,
			int shadowIndex
		) {
			vec4 shadowWorldPosition = vec4( vSunShadowWorldPosition.xyz + vSunShadowWorldNormal * sunLightShadow.shadowNormalBias, 1.0 );
			float viewDepth = vSunShadowWorldPosition.w;
			int cascadeOffset = shadowIndex * SUN_LIGHT_CASCADES;
			float shadow = 1.0;
			for ( int i = SUN_LIGHT_CASCADES - 1; i >= 0; i -- ) {
				vec4 cascade = sunShadowCascade[ cascadeOffset + i ];
				if ( viewDepth >= cascade.x && viewDepth < cascade.y ) {
					float cascadeShadow = getShadow(
						shadowMap,
						sunLightShadow.shadowMapSize,
						sunLightShadow.shadowIntensity,
						sunLightShadow.shadowBias,
						sunLightShadow.shadowRadius,
						sunShadowMatrix[ cascadeOffset + i ] * shadowWorldPosition
					);
					shadow = mix( cascadeShadow, shadow, smoothstep( cascade.z, cascade.y, viewDepth ) );
				}
			}
			return shadow;
		}
	#endif
	#if NUM_POINT_LIGHT_SHADOWS > 0
	#if defined( SHADOWMAP_TYPE_PCF )
	float getPointShadow( samplerCubeShadow shadowMap, vec2 shadowMapSize, float shadowIntensity, float shadowBias, float shadowRadius, vec4 shadowCoord, float shadowCameraNear, float shadowCameraFar ) {
		float shadow = 1.0;
		vec3 lightToPosition = shadowCoord.xyz;
		vec3 bd3D = normalize( lightToPosition );
		vec3 absVec = abs( lightToPosition );
		float viewSpaceZ = max( max( absVec.x, absVec.y ), absVec.z );
		if ( viewSpaceZ - shadowCameraFar <= 0.0 && viewSpaceZ - shadowCameraNear >= 0.0 ) {
			#ifdef USE_REVERSED_DEPTH_BUFFER
				float dp = ( shadowCameraNear * ( shadowCameraFar - viewSpaceZ ) ) / ( viewSpaceZ * ( shadowCameraFar - shadowCameraNear ) );
				dp -= shadowBias;
			#else
				float dp = ( shadowCameraFar * ( viewSpaceZ - shadowCameraNear ) ) / ( viewSpaceZ * ( shadowCameraFar - shadowCameraNear ) );
				dp += shadowBias;
			#endif
			float texelSize = shadowRadius / shadowMapSize.x;
			vec3 absDir = abs( bd3D );
			vec3 tangent = absDir.x > absDir.z ? vec3( 0.0, 1.0, 0.0 ) : vec3( 1.0, 0.0, 0.0 );
			tangent = normalize( cross( bd3D, tangent ) );
			vec3 bitangent = cross( bd3D, tangent );
			float phi = interleavedGradientNoise( gl_FragCoord.xy ) * PI2;
			vec2 sample0 = vogelDiskSample( 0, 5, phi );
			vec2 sample1 = vogelDiskSample( 1, 5, phi );
			vec2 sample2 = vogelDiskSample( 2, 5, phi );
			vec2 sample3 = vogelDiskSample( 3, 5, phi );
			vec2 sample4 = vogelDiskSample( 4, 5, phi );
			shadow = (
				texture( shadowMap, vec4( bd3D + ( tangent * sample0.x + bitangent * sample0.y ) * texelSize, dp ) ) +
				texture( shadowMap, vec4( bd3D + ( tangent * sample1.x + bitangent * sample1.y ) * texelSize, dp ) ) +
				texture( shadowMap, vec4( bd3D + ( tangent * sample2.x + bitangent * sample2.y ) * texelSize, dp ) ) +
				texture( shadowMap, vec4( bd3D + ( tangent * sample3.x + bitangent * sample3.y ) * texelSize, dp ) ) +
				texture( shadowMap, vec4( bd3D + ( tangent * sample4.x + bitangent * sample4.y ) * texelSize, dp ) )
			) * 0.2;
		}
		return mix( 1.0, shadow, shadowIntensity );
	}
	#elif defined( SHADOWMAP_TYPE_BASIC )
	float getPointShadow( samplerCube shadowMap, vec2 shadowMapSize, float shadowIntensity, float shadowBias, float shadowRadius, vec4 shadowCoord, float shadowCameraNear, float shadowCameraFar ) {
		float shadow = 1.0;
		vec3 lightToPosition = shadowCoord.xyz;
		vec3 absVec = abs( lightToPosition );
		float viewSpaceZ = max( max( absVec.x, absVec.y ), absVec.z );
		if ( viewSpaceZ - shadowCameraFar <= 0.0 && viewSpaceZ - shadowCameraNear >= 0.0 ) {
			float dp = ( shadowCameraFar * ( viewSpaceZ - shadowCameraNear ) ) / ( viewSpaceZ * ( shadowCameraFar - shadowCameraNear ) );
			dp += shadowBias;
			vec3 bd3D = normalize( lightToPosition );
			float depth = textureCube( shadowMap, bd3D ).r;
			#ifdef USE_REVERSED_DEPTH_BUFFER
				depth = 1.0 - depth;
			#endif
			shadow = step( dp, depth );
		}
		return mix( 1.0, shadow, shadowIntensity );
	}
	#endif
	#endif
#endif`,ZM=`#if NUM_SPOT_LIGHT_COORDS > 0
	uniform mat4 spotLightMatrix[ NUM_SPOT_LIGHT_COORDS ];
	varying vec4 vSpotLightCoord[ NUM_SPOT_LIGHT_COORDS ];
#endif
#ifdef USE_SHADOWMAP
	#if NUM_SUN_LIGHT_SHADOWS > 0
		varying vec4 vSunShadowWorldPosition;
		varying vec3 vSunShadowWorldNormal;
	#endif
	#if NUM_DIR_LIGHT_SHADOWS > 0
		uniform mat4 directionalShadowMatrix[ NUM_DIR_LIGHT_SHADOWS ];
		varying vec4 vDirectionalShadowCoord[ NUM_DIR_LIGHT_SHADOWS ];
		struct DirectionalLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
		};
		uniform DirectionalLightShadow directionalLightShadows[ NUM_DIR_LIGHT_SHADOWS ];
	#endif
	#if NUM_SPOT_LIGHT_SHADOWS > 0
		struct SpotLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
		};
		uniform SpotLightShadow spotLightShadows[ NUM_SPOT_LIGHT_SHADOWS ];
	#endif
	#if NUM_POINT_LIGHT_SHADOWS > 0
		uniform mat4 pointShadowMatrix[ NUM_POINT_LIGHT_SHADOWS ];
		varying vec4 vPointShadowCoord[ NUM_POINT_LIGHT_SHADOWS ];
		struct PointLightShadow {
			float shadowIntensity;
			float shadowBias;
			float shadowNormalBias;
			float shadowRadius;
			vec2 shadowMapSize;
			float shadowCameraNear;
			float shadowCameraFar;
		};
		uniform PointLightShadow pointLightShadows[ NUM_POINT_LIGHT_SHADOWS ];
	#endif
#endif`,KM=`#if ( defined( USE_SHADOWMAP ) && ( NUM_DIR_LIGHT_SHADOWS > 0 || NUM_SUN_LIGHT_SHADOWS > 0 || NUM_POINT_LIGHT_SHADOWS > 0 ) ) || ( NUM_SPOT_LIGHT_COORDS > 0 )
	#ifdef HAS_NORMAL
		vec3 shadowWorldNormal = transformNormalByInverseViewMatrix( transformedNormal, viewMatrix );
	#else
		vec3 shadowWorldNormal = vec3( 0.0 );
	#endif
	vec4 shadowWorldPosition;
#endif
#if defined( USE_SHADOWMAP )
	#if NUM_SUN_LIGHT_SHADOWS > 0
		vSunShadowWorldPosition = vec4( worldPosition.xyz, - mvPosition.z );
		vSunShadowWorldNormal = shadowWorldNormal;
	#endif
	#if NUM_DIR_LIGHT_SHADOWS > 0
		#pragma unroll_loop_start
		for ( int i = 0; i < NUM_DIR_LIGHT_SHADOWS; i ++ ) {
			shadowWorldPosition = worldPosition + vec4( shadowWorldNormal * directionalLightShadows[ i ].shadowNormalBias, 0 );
			vDirectionalShadowCoord[ i ] = directionalShadowMatrix[ i ] * shadowWorldPosition;
		}
		#pragma unroll_loop_end
	#endif
	#if NUM_POINT_LIGHT_SHADOWS > 0
		#pragma unroll_loop_start
		for ( int i = 0; i < NUM_POINT_LIGHT_SHADOWS; i ++ ) {
			shadowWorldPosition = worldPosition + vec4( shadowWorldNormal * pointLightShadows[ i ].shadowNormalBias, 0 );
			vPointShadowCoord[ i ] = pointShadowMatrix[ i ] * shadowWorldPosition;
		}
		#pragma unroll_loop_end
	#endif
#endif
#if NUM_SPOT_LIGHT_COORDS > 0
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_SPOT_LIGHT_COORDS; i ++ ) {
		shadowWorldPosition = worldPosition;
		#if ( defined( USE_SHADOWMAP ) && UNROLLED_LOOP_INDEX < NUM_SPOT_LIGHT_SHADOWS )
			shadowWorldPosition.xyz += shadowWorldNormal * spotLightShadows[ i ].shadowNormalBias;
		#endif
		vSpotLightCoord[ i ] = spotLightMatrix[ i ] * shadowWorldPosition;
	}
	#pragma unroll_loop_end
#endif`,JM=`float getShadowMask() {
	float shadow = 1.0;
	#ifdef USE_SHADOWMAP
	#if NUM_SUN_LIGHT_SHADOWS > 0
	SunLightShadow sunLight;
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_SUN_LIGHT_SHADOWS; i ++ ) {
		sunLight = sunLightShadows[ i ];
		shadow *= receiveShadow ? getSunShadow( sunShadowMap[ i ], sunLight, UNROLLED_LOOP_INDEX ) : 1.0;
	}
	#pragma unroll_loop_end
	#endif
	#if NUM_DIR_LIGHT_SHADOWS > 0
	DirectionalLightShadow directionalLight;
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_DIR_LIGHT_SHADOWS; i ++ ) {
		directionalLight = directionalLightShadows[ i ];
		shadow *= receiveShadow ? getShadow( directionalShadowMap[ i ], directionalLight.shadowMapSize, directionalLight.shadowIntensity, directionalLight.shadowBias, directionalLight.shadowRadius, vDirectionalShadowCoord[ i ] ) : 1.0;
	}
	#pragma unroll_loop_end
	#endif
	#if NUM_SPOT_LIGHT_SHADOWS > 0
	SpotLightShadow spotLight;
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_SPOT_LIGHT_SHADOWS; i ++ ) {
		spotLight = spotLightShadows[ i ];
		shadow *= receiveShadow ? getShadow( spotShadowMap[ i ], spotLight.shadowMapSize, spotLight.shadowIntensity, spotLight.shadowBias, spotLight.shadowRadius, vSpotLightCoord[ i ] ) : 1.0;
	}
	#pragma unroll_loop_end
	#endif
	#if NUM_POINT_LIGHT_SHADOWS > 0 && ( defined( SHADOWMAP_TYPE_PCF ) || defined( SHADOWMAP_TYPE_BASIC ) )
	PointLightShadow pointLight;
	#pragma unroll_loop_start
	for ( int i = 0; i < NUM_POINT_LIGHT_SHADOWS; i ++ ) {
		pointLight = pointLightShadows[ i ];
		shadow *= receiveShadow ? getPointShadow( pointShadowMap[ i ], pointLight.shadowMapSize, pointLight.shadowIntensity, pointLight.shadowBias, pointLight.shadowRadius, vPointShadowCoord[ i ], pointLight.shadowCameraNear, pointLight.shadowCameraFar ) : 1.0;
	}
	#pragma unroll_loop_end
	#endif
	#endif
	return shadow;
}`,QM=`#ifdef USE_SKINNING
	mat4 boneMatX = getBoneMatrix( skinIndex.x );
	mat4 boneMatY = getBoneMatrix( skinIndex.y );
	mat4 boneMatZ = getBoneMatrix( skinIndex.z );
	mat4 boneMatW = getBoneMatrix( skinIndex.w );
#endif`,e1=`#ifdef USE_SKINNING
	uniform mat4 bindMatrix;
	uniform mat4 bindMatrixInverse;
	uniform highp sampler2D boneTexture;
	mat4 getBoneMatrix( const in float i ) {
		int size = textureSize( boneTexture, 0 ).x;
		int j = int( i ) * 4;
		int x = j % size;
		int y = j / size;
		vec4 v1 = texelFetch( boneTexture, ivec2( x, y ), 0 );
		vec4 v2 = texelFetch( boneTexture, ivec2( x + 1, y ), 0 );
		vec4 v3 = texelFetch( boneTexture, ivec2( x + 2, y ), 0 );
		vec4 v4 = texelFetch( boneTexture, ivec2( x + 3, y ), 0 );
		return mat4( v1, v2, v3, v4 );
	}
#endif`,t1=`#ifdef USE_SKINNING
	vec4 skinVertex = bindMatrix * vec4( transformed, 1.0 );
	vec4 skinned = vec4( 0.0 );
	skinned += boneMatX * skinVertex * skinWeight.x;
	skinned += boneMatY * skinVertex * skinWeight.y;
	skinned += boneMatZ * skinVertex * skinWeight.z;
	skinned += boneMatW * skinVertex * skinWeight.w;
	transformed = ( bindMatrixInverse * skinned ).xyz;
#endif`,n1=`#ifdef USE_SKINNING
	mat4 skinMatrix = mat4( 0.0 );
	skinMatrix += skinWeight.x * boneMatX;
	skinMatrix += skinWeight.y * boneMatY;
	skinMatrix += skinWeight.z * boneMatZ;
	skinMatrix += skinWeight.w * boneMatW;
	skinMatrix = bindMatrixInverse * skinMatrix * bindMatrix;
	objectNormal = vec4( skinMatrix * vec4( objectNormal, 0.0 ) ).xyz;
	#ifdef USE_TANGENT
		objectTangent = vec4( skinMatrix * vec4( objectTangent, 0.0 ) ).xyz;
	#endif
#endif`,i1=`float specularStrength;
#ifdef USE_SPECULARMAP
	vec4 texelSpecular = texture2D( specularMap, vSpecularMapUv );
	specularStrength = texelSpecular.r;
#else
	specularStrength = 1.0;
#endif`,r1=`#ifdef USE_SPECULARMAP
	uniform sampler2D specularMap;
#endif`,s1=`#if defined( TONE_MAPPING )
	gl_FragColor.rgb = toneMapping( gl_FragColor.rgb );
#endif`,o1=`#ifndef saturate
#define saturate( a ) clamp( a, 0.0, 1.0 )
#endif
uniform float toneMappingExposure;
vec3 LinearToneMapping( vec3 color ) {
	return saturate( toneMappingExposure * color );
}
vec3 ReinhardToneMapping( vec3 color ) {
	color *= toneMappingExposure;
	return saturate( color / ( vec3( 1.0 ) + color ) );
}
vec3 CineonToneMapping( vec3 color ) {
	color *= toneMappingExposure;
	color = max( vec3( 0.0 ), color - 0.004 );
	return pow( ( color * ( 6.2 * color + 0.5 ) ) / ( color * ( 6.2 * color + 1.7 ) + 0.06 ), vec3( 2.2 ) );
}
vec3 RRTAndODTFit( vec3 v ) {
	vec3 a = v * ( v + 0.0245786 ) - 0.000090537;
	vec3 b = v * ( 0.983729 * v + 0.4329510 ) + 0.238081;
	return a / b;
}
vec3 ACESFilmicToneMapping( vec3 color ) {
	const mat3 ACESInputMat = mat3(
		vec3( 0.59719, 0.07600, 0.02840 ),		vec3( 0.35458, 0.90834, 0.13383 ),
		vec3( 0.04823, 0.01566, 0.83777 )
	);
	const mat3 ACESOutputMat = mat3(
		vec3(  1.60475, -0.10208, -0.00327 ),		vec3( -0.53108,  1.10813, -0.07276 ),
		vec3( -0.07367, -0.00605,  1.07602 )
	);
	color *= toneMappingExposure / 0.6;
	color = ACESInputMat * color;
	color = RRTAndODTFit( color );
	color = ACESOutputMat * color;
	return saturate( color );
}
const mat3 LINEAR_REC2020_TO_LINEAR_SRGB = mat3(
	vec3( 1.6605, - 0.1246, - 0.0182 ),
	vec3( - 0.5876, 1.1329, - 0.1006 ),
	vec3( - 0.0728, - 0.0083, 1.1187 )
);
const mat3 LINEAR_SRGB_TO_LINEAR_REC2020 = mat3(
	vec3( 0.6274, 0.0691, 0.0164 ),
	vec3( 0.3293, 0.9195, 0.0880 ),
	vec3( 0.0433, 0.0113, 0.8956 )
);
vec3 agxDefaultContrastApprox( vec3 x ) {
	vec3 x2 = x * x;
	vec3 x4 = x2 * x2;
	return + 15.5 * x4 * x2
		- 40.14 * x4 * x
		+ 31.96 * x4
		- 6.868 * x2 * x
		+ 0.4298 * x2
		+ 0.1191 * x
		- 0.00232;
}
vec3 AgXToneMapping( vec3 color ) {
	const mat3 AgXInsetMatrix = mat3(
		vec3( 0.856627153315983, 0.137318972929847, 0.11189821299995 ),
		vec3( 0.0951212405381588, 0.761241990602591, 0.0767994186031903 ),
		vec3( 0.0482516061458583, 0.101439036467562, 0.811302368396859 )
	);
	const mat3 AgXOutsetMatrix = mat3(
		vec3( 1.1271005818144368, - 0.1413297634984383, - 0.14132976349843826 ),
		vec3( - 0.11060664309660323, 1.157823702216272, - 0.11060664309660294 ),
		vec3( - 0.016493938717834573, - 0.016493938717834257, 1.2519364065950405 )
	);
	const float AgxMinEv = - 12.47393;	const float AgxMaxEv = 4.026069;
	color *= toneMappingExposure;
	color = LINEAR_SRGB_TO_LINEAR_REC2020 * color;
	color = AgXInsetMatrix * color;
	color = max( color, 1e-10 );	color = log2( color );
	color = ( color - AgxMinEv ) / ( AgxMaxEv - AgxMinEv );
	color = clamp( color, 0.0, 1.0 );
	color = agxDefaultContrastApprox( color );
	color = AgXOutsetMatrix * color;
	color = pow( max( vec3( 0.0 ), color ), vec3( 2.2 ) );
	color = LINEAR_REC2020_TO_LINEAR_SRGB * color;
	color = clamp( color, 0.0, 1.0 );
	return color;
}
vec3 NeutralToneMapping( vec3 color ) {
	const float StartCompression = 0.8 - 0.04;
	const float Desaturation = 0.15;
	color *= toneMappingExposure;
	float x = min( color.r, min( color.g, color.b ) );
	float offset = x < 0.08 ? x - 6.25 * x * x : 0.04;
	color -= offset;
	float peak = max( color.r, max( color.g, color.b ) );
	if ( peak < StartCompression ) return color;
	float d = 1. - StartCompression;
	float newPeak = 1. - d * d / ( peak + d - StartCompression );
	color *= newPeak / peak;
	float g = 1. - 1. / ( Desaturation * ( peak - newPeak ) + 1. );
	return mix( color, vec3( newPeak ), g );
}
vec3 CustomToneMapping( vec3 color ) { return color; }`,a1=`#ifdef USE_TRANSMISSION
	material.transmission = transmission;
	material.transmissionAlpha = 1.0;
	material.thickness = thickness;
	material.attenuationDistance = attenuationDistance;
	material.attenuationColor = attenuationColor;
	#ifdef USE_TRANSMISSIONMAP
		material.transmission *= texture2D( transmissionMap, vTransmissionMapUv ).r;
	#endif
	#ifdef USE_THICKNESSMAP
		material.thickness *= texture2D( thicknessMap, vThicknessMapUv ).g;
	#endif
	vec3 pos = vWorldPosition;
	vec3 v = normalize( cameraPosition - pos );
	vec3 n = transformNormalByInverseViewMatrix( normal, viewMatrix );
	vec4 transmitted = getIBLVolumeRefraction(
		n, v, material.roughness, material.diffuseContribution, material.specularColorBlended, material.specularF90,
		pos, modelMatrix, viewMatrix, projectionMatrix, material.dispersion, material.ior, material.thickness,
		material.attenuationColor, material.attenuationDistance );
	material.transmissionAlpha = mix( material.transmissionAlpha, transmitted.a, material.transmission );
	totalDiffuse = mix( totalDiffuse, transmitted.rgb, material.transmission );
#endif`,l1=`#ifdef USE_TRANSMISSION
	uniform float transmission;
	uniform float thickness;
	uniform float attenuationDistance;
	uniform vec3 attenuationColor;
	#ifdef USE_TRANSMISSIONMAP
		uniform sampler2D transmissionMap;
	#endif
	#ifdef USE_THICKNESSMAP
		uniform sampler2D thicknessMap;
	#endif
	uniform vec2 transmissionSamplerSize;
	uniform sampler2D transmissionSamplerMap;
	uniform mat4 modelMatrix;
	uniform mat4 projectionMatrix;
	varying vec3 vWorldPosition;
	float w0( float a ) {
		return ( 1.0 / 6.0 ) * ( a * ( a * ( - a + 3.0 ) - 3.0 ) + 1.0 );
	}
	float w1( float a ) {
		return ( 1.0 / 6.0 ) * ( a *  a * ( 3.0 * a - 6.0 ) + 4.0 );
	}
	float w2( float a ){
		return ( 1.0 / 6.0 ) * ( a * ( a * ( - 3.0 * a + 3.0 ) + 3.0 ) + 1.0 );
	}
	float w3( float a ) {
		return ( 1.0 / 6.0 ) * ( a * a * a );
	}
	float g0( float a ) {
		return w0( a ) + w1( a );
	}
	float g1( float a ) {
		return w2( a ) + w3( a );
	}
	float h0( float a ) {
		return - 1.0 + w1( a ) / ( w0( a ) + w1( a ) );
	}
	float h1( float a ) {
		return 1.0 + w3( a ) / ( w2( a ) + w3( a ) );
	}
	vec4 bicubic( sampler2D tex, vec2 uv, vec4 texelSize, float lod ) {
		uv = uv * texelSize.zw + 0.5;
		vec2 iuv = floor( uv );
		vec2 fuv = fract( uv );
		float g0x = g0( fuv.x );
		float g1x = g1( fuv.x );
		float h0x = h0( fuv.x );
		float h1x = h1( fuv.x );
		float h0y = h0( fuv.y );
		float h1y = h1( fuv.y );
		vec2 p0 = ( vec2( iuv.x + h0x, iuv.y + h0y ) - 0.5 ) * texelSize.xy;
		vec2 p1 = ( vec2( iuv.x + h1x, iuv.y + h0y ) - 0.5 ) * texelSize.xy;
		vec2 p2 = ( vec2( iuv.x + h0x, iuv.y + h1y ) - 0.5 ) * texelSize.xy;
		vec2 p3 = ( vec2( iuv.x + h1x, iuv.y + h1y ) - 0.5 ) * texelSize.xy;
		return g0( fuv.y ) * ( g0x * textureLod( tex, p0, lod ) + g1x * textureLod( tex, p1, lod ) ) +
			g1( fuv.y ) * ( g0x * textureLod( tex, p2, lod ) + g1x * textureLod( tex, p3, lod ) );
	}
	vec4 textureBicubic( sampler2D sampler, vec2 uv, float lod ) {
		vec2 fLodSize = vec2( textureSize( sampler, int( lod ) ) );
		vec2 cLodSize = vec2( textureSize( sampler, int( lod + 1.0 ) ) );
		vec2 fLodSizeInv = 1.0 / fLodSize;
		vec2 cLodSizeInv = 1.0 / cLodSize;
		vec4 fSample = bicubic( sampler, uv, vec4( fLodSizeInv, fLodSize ), floor( lod ) );
		vec4 cSample = bicubic( sampler, uv, vec4( cLodSizeInv, cLodSize ), ceil( lod ) );
		return mix( fSample, cSample, fract( lod ) );
	}
	vec3 getVolumeTransmissionRay( const in vec3 n, const in vec3 v, const in float thickness, const in float ior, const in mat4 modelMatrix ) {
		vec3 refractionVector = refract( - v, normalize( n ), 1.0 / ior );
		vec3 modelScale;
		modelScale.x = length( vec3( modelMatrix[ 0 ].xyz ) );
		modelScale.y = length( vec3( modelMatrix[ 1 ].xyz ) );
		modelScale.z = length( vec3( modelMatrix[ 2 ].xyz ) );
		return normalize( refractionVector ) * thickness * modelScale;
	}
	float applyIorToRoughness( const in float roughness, const in float ior ) {
		return roughness * clamp( ior * 2.0 - 2.0, 0.0, 1.0 );
	}
	vec4 getTransmissionSample( const in vec2 fragCoord, const in float roughness, const in float ior ) {
		float lod = log2( transmissionSamplerSize.x ) * applyIorToRoughness( roughness, ior );
		return textureBicubic( transmissionSamplerMap, fragCoord.xy, lod );
	}
	vec3 volumeAttenuation( const in float transmissionDistance, const in vec3 attenuationColor, const in float attenuationDistance ) {
		if ( isinf( attenuationDistance ) ) {
			return vec3( 1.0 );
		} else {
			vec3 attenuationCoefficient = -log( attenuationColor ) / attenuationDistance;
			vec3 transmittance = exp( - attenuationCoefficient * transmissionDistance );			return transmittance;
		}
	}
	vec4 getIBLVolumeRefraction( const in vec3 n, const in vec3 v, const in float roughness, const in vec3 diffuseColor,
		const in vec3 specularColor, const in float specularF90, const in vec3 position, const in mat4 modelMatrix,
		const in mat4 viewMatrix, const in mat4 projMatrix, const in float dispersion, const in float ior, const in float thickness,
		const in vec3 attenuationColor, const in float attenuationDistance ) {
		vec4 transmittedLight;
		vec3 transmittance;
		#ifdef USE_DISPERSION
			float halfSpread = ( ior - 1.0 ) * 0.025 * dispersion;
			vec3 iors = vec3( ior - halfSpread, ior, ior + halfSpread );
			for ( int i = 0; i < 3; i ++ ) {
				vec3 transmissionRay = getVolumeTransmissionRay( n, v, thickness, iors[ i ], modelMatrix );
				vec3 refractedRayExit = position + transmissionRay;
				vec4 ndcPos = projMatrix * viewMatrix * vec4( refractedRayExit, 1.0 );
				vec2 refractionCoords = ndcPos.xy / ndcPos.w;
				refractionCoords += 1.0;
				refractionCoords /= 2.0;
				vec4 transmissionSample = getTransmissionSample( refractionCoords, roughness, iors[ i ] );
				transmittedLight[ i ] = transmissionSample[ i ];
				transmittedLight.a += transmissionSample.a;
				transmittance[ i ] = diffuseColor[ i ] * volumeAttenuation( length( transmissionRay ), attenuationColor, attenuationDistance )[ i ];
			}
			transmittedLight.a /= 3.0;
		#else
			vec3 transmissionRay = getVolumeTransmissionRay( n, v, thickness, ior, modelMatrix );
			vec3 refractedRayExit = position + transmissionRay;
			vec4 ndcPos = projMatrix * viewMatrix * vec4( refractedRayExit, 1.0 );
			vec2 refractionCoords = ndcPos.xy / ndcPos.w;
			refractionCoords += 1.0;
			refractionCoords /= 2.0;
			transmittedLight = getTransmissionSample( refractionCoords, roughness, ior );
			transmittance = diffuseColor * volumeAttenuation( length( transmissionRay ), attenuationColor, attenuationDistance );
		#endif
		vec3 attenuatedColor = transmittance * transmittedLight.rgb;
		vec3 F = EnvironmentBRDF( n, v, specularColor, specularF90, roughness );
		float transmittanceFactor = ( transmittance.r + transmittance.g + transmittance.b ) / 3.0;
		return vec4( ( 1.0 - F ) * attenuatedColor, 1.0 - ( 1.0 - transmittedLight.a ) * transmittanceFactor );
	}
#endif`,c1=`#if defined( USE_UV ) || defined( USE_ANISOTROPY )
	varying vec2 vUv;
#endif
#ifdef USE_MAP
	varying vec2 vMapUv;
#endif
#ifdef USE_ALPHAMAP
	varying vec2 vAlphaMapUv;
#endif
#ifdef USE_LIGHTMAP
	varying vec2 vLightMapUv;
#endif
#ifdef USE_AOMAP
	varying vec2 vAoMapUv;
#endif
#ifdef USE_BUMPMAP
	varying vec2 vBumpMapUv;
#endif
#ifdef USE_NORMALMAP
	varying vec2 vNormalMapUv;
#endif
#ifdef USE_EMISSIVEMAP
	varying vec2 vEmissiveMapUv;
#endif
#ifdef USE_METALNESSMAP
	varying vec2 vMetalnessMapUv;
#endif
#ifdef USE_ROUGHNESSMAP
	varying vec2 vRoughnessMapUv;
#endif
#ifdef USE_ANISOTROPYMAP
	varying vec2 vAnisotropyMapUv;
#endif
#ifdef USE_CLEARCOATMAP
	varying vec2 vClearcoatMapUv;
#endif
#ifdef USE_CLEARCOAT_NORMALMAP
	varying vec2 vClearcoatNormalMapUv;
#endif
#ifdef USE_CLEARCOAT_ROUGHNESSMAP
	varying vec2 vClearcoatRoughnessMapUv;
#endif
#ifdef USE_IRIDESCENCEMAP
	varying vec2 vIridescenceMapUv;
#endif
#ifdef USE_IRIDESCENCE_THICKNESSMAP
	varying vec2 vIridescenceThicknessMapUv;
#endif
#ifdef USE_SHEEN_COLORMAP
	varying vec2 vSheenColorMapUv;
#endif
#ifdef USE_SHEEN_ROUGHNESSMAP
	varying vec2 vSheenRoughnessMapUv;
#endif
#ifdef USE_SPECULARMAP
	varying vec2 vSpecularMapUv;
#endif
#ifdef USE_SPECULAR_COLORMAP
	varying vec2 vSpecularColorMapUv;
#endif
#ifdef USE_SPECULAR_INTENSITYMAP
	varying vec2 vSpecularIntensityMapUv;
#endif
#ifdef USE_TRANSMISSIONMAP
	uniform mat3 transmissionMapTransform;
	varying vec2 vTransmissionMapUv;
#endif
#ifdef USE_THICKNESSMAP
	uniform mat3 thicknessMapTransform;
	varying vec2 vThicknessMapUv;
#endif`,u1=`#if defined( USE_UV ) || defined( USE_ANISOTROPY )
	varying vec2 vUv;
#endif
#ifdef USE_MAP
	uniform mat3 mapTransform;
	varying vec2 vMapUv;
#endif
#ifdef USE_ALPHAMAP
	uniform mat3 alphaMapTransform;
	varying vec2 vAlphaMapUv;
#endif
#ifdef USE_LIGHTMAP
	uniform mat3 lightMapTransform;
	varying vec2 vLightMapUv;
#endif
#ifdef USE_AOMAP
	uniform mat3 aoMapTransform;
	varying vec2 vAoMapUv;
#endif
#ifdef USE_BUMPMAP
	uniform mat3 bumpMapTransform;
	varying vec2 vBumpMapUv;
#endif
#ifdef USE_NORMALMAP
	uniform mat3 normalMapTransform;
	varying vec2 vNormalMapUv;
#endif
#ifdef USE_DISPLACEMENTMAP
	uniform mat3 displacementMapTransform;
	varying vec2 vDisplacementMapUv;
#endif
#ifdef USE_EMISSIVEMAP
	uniform mat3 emissiveMapTransform;
	varying vec2 vEmissiveMapUv;
#endif
#ifdef USE_METALNESSMAP
	uniform mat3 metalnessMapTransform;
	varying vec2 vMetalnessMapUv;
#endif
#ifdef USE_ROUGHNESSMAP
	uniform mat3 roughnessMapTransform;
	varying vec2 vRoughnessMapUv;
#endif
#ifdef USE_ANISOTROPYMAP
	uniform mat3 anisotropyMapTransform;
	varying vec2 vAnisotropyMapUv;
#endif
#ifdef USE_CLEARCOATMAP
	uniform mat3 clearcoatMapTransform;
	varying vec2 vClearcoatMapUv;
#endif
#ifdef USE_CLEARCOAT_NORMALMAP
	uniform mat3 clearcoatNormalMapTransform;
	varying vec2 vClearcoatNormalMapUv;
#endif
#ifdef USE_CLEARCOAT_ROUGHNESSMAP
	uniform mat3 clearcoatRoughnessMapTransform;
	varying vec2 vClearcoatRoughnessMapUv;
#endif
#ifdef USE_SHEEN_COLORMAP
	uniform mat3 sheenColorMapTransform;
	varying vec2 vSheenColorMapUv;
#endif
#ifdef USE_SHEEN_ROUGHNESSMAP
	uniform mat3 sheenRoughnessMapTransform;
	varying vec2 vSheenRoughnessMapUv;
#endif
#ifdef USE_IRIDESCENCEMAP
	uniform mat3 iridescenceMapTransform;
	varying vec2 vIridescenceMapUv;
#endif
#ifdef USE_IRIDESCENCE_THICKNESSMAP
	uniform mat3 iridescenceThicknessMapTransform;
	varying vec2 vIridescenceThicknessMapUv;
#endif
#ifdef USE_SPECULARMAP
	uniform mat3 specularMapTransform;
	varying vec2 vSpecularMapUv;
#endif
#ifdef USE_SPECULAR_COLORMAP
	uniform mat3 specularColorMapTransform;
	varying vec2 vSpecularColorMapUv;
#endif
#ifdef USE_SPECULAR_INTENSITYMAP
	uniform mat3 specularIntensityMapTransform;
	varying vec2 vSpecularIntensityMapUv;
#endif
#ifdef USE_TRANSMISSIONMAP
	uniform mat3 transmissionMapTransform;
	varying vec2 vTransmissionMapUv;
#endif
#ifdef USE_THICKNESSMAP
	uniform mat3 thicknessMapTransform;
	varying vec2 vThicknessMapUv;
#endif`,h1=`#if defined( USE_UV ) || defined( USE_ANISOTROPY )
	vUv = vec3( uv, 1 ).xy;
#endif
#ifdef USE_MAP
	vMapUv = ( mapTransform * vec3( MAP_UV, 1 ) ).xy;
#endif
#ifdef USE_ALPHAMAP
	vAlphaMapUv = ( alphaMapTransform * vec3( ALPHAMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_LIGHTMAP
	vLightMapUv = ( lightMapTransform * vec3( LIGHTMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_AOMAP
	vAoMapUv = ( aoMapTransform * vec3( AOMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_BUMPMAP
	vBumpMapUv = ( bumpMapTransform * vec3( BUMPMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_NORMALMAP
	vNormalMapUv = ( normalMapTransform * vec3( NORMALMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_DISPLACEMENTMAP
	vDisplacementMapUv = ( displacementMapTransform * vec3( DISPLACEMENTMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_EMISSIVEMAP
	vEmissiveMapUv = ( emissiveMapTransform * vec3( EMISSIVEMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_METALNESSMAP
	vMetalnessMapUv = ( metalnessMapTransform * vec3( METALNESSMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_ROUGHNESSMAP
	vRoughnessMapUv = ( roughnessMapTransform * vec3( ROUGHNESSMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_ANISOTROPYMAP
	vAnisotropyMapUv = ( anisotropyMapTransform * vec3( ANISOTROPYMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_CLEARCOATMAP
	vClearcoatMapUv = ( clearcoatMapTransform * vec3( CLEARCOATMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_CLEARCOAT_NORMALMAP
	vClearcoatNormalMapUv = ( clearcoatNormalMapTransform * vec3( CLEARCOAT_NORMALMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_CLEARCOAT_ROUGHNESSMAP
	vClearcoatRoughnessMapUv = ( clearcoatRoughnessMapTransform * vec3( CLEARCOAT_ROUGHNESSMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_IRIDESCENCEMAP
	vIridescenceMapUv = ( iridescenceMapTransform * vec3( IRIDESCENCEMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_IRIDESCENCE_THICKNESSMAP
	vIridescenceThicknessMapUv = ( iridescenceThicknessMapTransform * vec3( IRIDESCENCE_THICKNESSMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_SHEEN_COLORMAP
	vSheenColorMapUv = ( sheenColorMapTransform * vec3( SHEEN_COLORMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_SHEEN_ROUGHNESSMAP
	vSheenRoughnessMapUv = ( sheenRoughnessMapTransform * vec3( SHEEN_ROUGHNESSMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_SPECULARMAP
	vSpecularMapUv = ( specularMapTransform * vec3( SPECULARMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_SPECULAR_COLORMAP
	vSpecularColorMapUv = ( specularColorMapTransform * vec3( SPECULAR_COLORMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_SPECULAR_INTENSITYMAP
	vSpecularIntensityMapUv = ( specularIntensityMapTransform * vec3( SPECULAR_INTENSITYMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_TRANSMISSIONMAP
	vTransmissionMapUv = ( transmissionMapTransform * vec3( TRANSMISSIONMAP_UV, 1 ) ).xy;
#endif
#ifdef USE_THICKNESSMAP
	vThicknessMapUv = ( thicknessMapTransform * vec3( THICKNESSMAP_UV, 1 ) ).xy;
#endif`,f1=`#if defined( USE_ENVMAP ) || defined( DISTANCE ) || defined ( USE_SHADOWMAP ) || defined ( USE_TRANSMISSION ) || NUM_SPOT_LIGHT_COORDS > 0
	vec4 worldPosition = vec4( transformed, 1.0 );
	#ifdef USE_BATCHING
		worldPosition = batchingMatrix * worldPosition;
	#endif
	#ifdef USE_INSTANCING
		worldPosition = instanceMatrix * worldPosition;
	#endif
	worldPosition = modelMatrix * worldPosition;
#endif`,d1=`varying vec2 vUv;
uniform mat3 uvTransform;
void main() {
	vUv = ( uvTransform * vec3( uv, 1 ) ).xy;
	gl_Position = vec4( position.xy, 1.0, 1.0 );
}`,p1=`uniform sampler2D t2D;
uniform float backgroundIntensity;
varying vec2 vUv;
void main() {
	vec4 texColor = texture2D( t2D, vUv );
	#ifdef DECODE_VIDEO_TEXTURE
		texColor = vec4( mix( pow( texColor.rgb * 0.9478672986 + vec3( 0.0521327014 ), vec3( 2.4 ) ), texColor.rgb * 0.0773993808, vec3( lessThanEqual( texColor.rgb, vec3( 0.04045 ) ) ) ), texColor.w );
	#endif
	texColor.rgb *= backgroundIntensity;
	gl_FragColor = texColor;
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
}`,m1=`varying vec3 vWorldDirection;
#include <common>
void main() {
	vWorldDirection = transformDirection( position, modelMatrix );
	#include <begin_vertex>
	#include <project_vertex>
	gl_Position.z = gl_Position.w;
}`,g1=`#ifdef ENVMAP_TYPE_CUBE
	uniform samplerCube envMap;
#elif defined( ENVMAP_TYPE_CUBE_UV )
	uniform sampler2D envMap;
#endif
uniform float backgroundBlurriness;
uniform float backgroundIntensity;
uniform mat3 backgroundRotation;
varying vec3 vWorldDirection;
#include <cube_uv_reflection_fragment>
void main() {
	#ifdef ENVMAP_TYPE_CUBE
		vec4 texColor = textureCube( envMap, backgroundRotation * vWorldDirection );
	#elif defined( ENVMAP_TYPE_CUBE_UV )
		vec4 texColor = textureCubeUV( envMap, backgroundRotation * vWorldDirection, backgroundBlurriness );
	#else
		vec4 texColor = vec4( 0.0, 0.0, 0.0, 1.0 );
	#endif
	texColor.rgb *= backgroundIntensity;
	gl_FragColor = texColor;
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
}`,_1=`varying vec3 vWorldDirection;
#include <common>
void main() {
	vWorldDirection = transformDirection( position, modelMatrix );
	#include <begin_vertex>
	#include <project_vertex>
	gl_Position.z = gl_Position.w;
}`,v1=`uniform samplerCube tCube;
uniform float tFlip;
uniform float opacity;
varying vec3 vWorldDirection;
void main() {
	vec4 texColor = textureCube( tCube, vec3( tFlip * vWorldDirection.x, vWorldDirection.yz ) );
	gl_FragColor = texColor;
	gl_FragColor.a *= opacity;
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
}`,y1=`#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
varying vec2 vHighPrecisionZW;
void main() {
	#include <uv_vertex>
	#include <batching_vertex>
	#include <skinbase_vertex>
	#include <morphinstance_vertex>
	#ifdef USE_DISPLACEMENTMAP
		#include <beginnormal_vertex>
		#include <morphnormal_vertex>
		#include <skinnormal_vertex>
	#endif
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	vHighPrecisionZW = gl_Position.zw;
}`,x1=`#if DEPTH_PACKING == 3200
	uniform float opacity;
#endif
#include <common>
#include <packing>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
varying vec2 vHighPrecisionZW;
void main() {
	vec4 diffuseColor = vec4( 1.0 );
	#include <clipping_planes_fragment>
	#if DEPTH_PACKING == 3200
		diffuseColor.a = opacity;
	#endif
	#include <map_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <logdepthbuf_fragment>
	#ifdef USE_REVERSED_DEPTH_BUFFER
		float fragCoordZ = vHighPrecisionZW[ 0 ] / vHighPrecisionZW[ 1 ];
	#else
		float fragCoordZ = 0.5 * vHighPrecisionZW[ 0 ] / vHighPrecisionZW[ 1 ] + 0.5;
	#endif
	#if DEPTH_PACKING == 3200
		gl_FragColor = vec4( vec3( 1.0 - fragCoordZ ), opacity );
	#elif DEPTH_PACKING == 3201
		gl_FragColor = packDepthToRGBA( fragCoordZ );
	#elif DEPTH_PACKING == 3202
		gl_FragColor = vec4( packDepthToRGB( fragCoordZ ), 1.0 );
	#elif DEPTH_PACKING == 3203
		gl_FragColor = vec4( packDepthToRG( fragCoordZ ), 0.0, 1.0 );
	#endif
}`,b1=`#define DISTANCE
varying vec3 vWorldPosition;
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <batching_vertex>
	#include <skinbase_vertex>
	#include <morphinstance_vertex>
	#ifdef USE_DISPLACEMENTMAP
		#include <beginnormal_vertex>
		#include <morphnormal_vertex>
		#include <skinnormal_vertex>
	#endif
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <worldpos_vertex>
	#include <clipping_planes_vertex>
	vWorldPosition = worldPosition.xyz;
}`,S1=`#define DISTANCE
uniform vec3 referencePosition;
uniform float nearDistance;
uniform float farDistance;
varying vec3 vWorldPosition;
#include <common>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( 1.0 );
	#include <clipping_planes_fragment>
	#include <map_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	float dist = length( vWorldPosition - referencePosition );
	dist = ( dist - nearDistance ) / ( farDistance - nearDistance );
	dist = saturate( dist );
	gl_FragColor = vec4( dist, 0.0, 0.0, 1.0 );
}`,w1=`varying vec3 vWorldDirection;
#include <common>
void main() {
	vWorldDirection = transformDirection( position, modelMatrix );
	#include <begin_vertex>
	#include <project_vertex>
}`,M1=`uniform sampler2D tEquirect;
varying vec3 vWorldDirection;
#include <common>
void main() {
	vec3 direction = normalize( vWorldDirection );
	vec2 sampleUV = equirectUv( direction );
	gl_FragColor = texture2D( tEquirect, sampleUV );
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
}`,E1=`uniform float scale;
attribute float lineDistance;
varying float vLineDistance;
#include <common>
#include <uv_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <morphtarget_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	vLineDistance = scale * lineDistance;
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	#include <fog_vertex>
}`,A1=`uniform vec3 diffuse;
uniform float opacity;
uniform float dashSize;
uniform float totalSize;
varying float vLineDistance;
#include <common>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <fog_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	if ( mod( vLineDistance, totalSize ) > dashSize ) {
		discard;
	}
	vec3 outgoingLight = vec3( 0.0 );
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	outgoingLight = diffuseColor.rgb;
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
}`,T1=`#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <envmap_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#if defined ( USE_ENVMAP ) || defined ( USE_SKINNING )
		#include <beginnormal_vertex>
		#include <morphnormal_vertex>
		#include <skinbase_vertex>
		#include <skinnormal_vertex>
		#include <defaultnormal_vertex>
	#endif
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	#include <worldpos_vertex>
	#include <envmap_vertex>
	#include <fog_vertex>
}`,C1=`uniform vec3 diffuse;
uniform float opacity;
#ifndef FLAT_SHADED
	varying vec3 vNormal;
#endif
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <aomap_pars_fragment>
#include <lightmap_pars_fragment>
#include <envmap_common_pars_fragment>
#include <envmap_pars_fragment>
#include <fog_pars_fragment>
#include <specularmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <specularmap_fragment>
	ReflectedLight reflectedLight = ReflectedLight( vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ) );
	#ifdef USE_LIGHTMAP
		vec4 lightMapTexel = texture2D( lightMap, vLightMapUv );
		reflectedLight.indirectDiffuse += lightMapTexel.rgb * lightMapIntensity * RECIPROCAL_PI;
	#else
		reflectedLight.indirectDiffuse += vec3( 1.0 );
	#endif
	#include <aomap_fragment>
	reflectedLight.indirectDiffuse *= diffuseColor.rgb;
	vec3 outgoingLight = reflectedLight.indirectDiffuse;
	#include <envmap_fragment>
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,R1=`#define LAMBERT
varying vec3 vViewPosition;
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <envmap_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <shadowmap_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	vViewPosition = - mvPosition.xyz;
	#include <worldpos_vertex>
	#include <envmap_vertex>
	#include <shadowmap_vertex>
	#include <fog_vertex>
}`,P1=`#define LAMBERT
uniform vec3 diffuse;
uniform vec3 emissive;
uniform float opacity;
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <aomap_pars_fragment>
#include <lightmap_pars_fragment>
#include <emissivemap_pars_fragment>
#include <cube_uv_reflection_fragment>
#include <envmap_common_pars_fragment>
#include <envmap_pars_fragment>
#include <envmap_physical_pars_fragment>
#include <fog_pars_fragment>
#include <bsdfs>
#include <lights_pars_begin>
#include <normal_pars_fragment>
#include <lights_lambert_pars_fragment>
#include <shadowmap_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <specularmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	ReflectedLight reflectedLight = ReflectedLight( vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ) );
	vec3 totalEmissiveRadiance = emissive;
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <specularmap_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	#include <emissivemap_fragment>
	#include <lights_lambert_fragment>
	#include <lights_fragment_begin>
	#include <lights_fragment_maps>
	#include <lights_fragment_end>
	#include <aomap_fragment>
	vec3 outgoingLight = reflectedLight.directDiffuse + reflectedLight.indirectDiffuse + totalEmissiveRadiance;
	#include <envmap_fragment>
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,I1=`#define MATCAP
varying vec3 vViewPosition;
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <color_pars_vertex>
#include <displacementmap_pars_vertex>
#include <fog_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	#include <fog_vertex>
	vViewPosition = - mvPosition.xyz;
}`,L1=`#define MATCAP
uniform vec3 diffuse;
uniform float opacity;
uniform sampler2D matcap;
varying vec3 vViewPosition;
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <fog_pars_fragment>
#include <normal_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	vec3 viewDir = normalize( vViewPosition );
	vec3 x = normalize( vec3( viewDir.z, 0.0, - viewDir.x ) );
	vec3 y = cross( viewDir, x );
	vec2 uv = vec2( dot( x, normal ), dot( y, normal ) ) * 0.495 + 0.5;
	#ifdef USE_MATCAP
		vec4 matcapColor = texture2D( matcap, uv );
	#else
		vec4 matcapColor = vec4( vec3( mix( 0.2, 0.8, uv.y ) ), 1.0 );
	#endif
	vec3 outgoingLight = diffuseColor.rgb * matcapColor.rgb;
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,D1=`#define NORMAL
#if defined( FLAT_SHADED ) || defined( USE_BUMPMAP ) || defined( USE_NORMALMAP_TANGENTSPACE )
	varying vec3 vViewPosition;
#endif
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphinstance_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
#if defined( FLAT_SHADED ) || defined( USE_BUMPMAP ) || defined( USE_NORMALMAP_TANGENTSPACE )
	vViewPosition = - mvPosition.xyz;
#endif
}`,O1=`#define NORMAL
uniform float opacity;
#if defined( FLAT_SHADED ) || defined( USE_BUMPMAP ) || defined( USE_NORMALMAP_TANGENTSPACE )
	varying vec3 vViewPosition;
#endif
#include <uv_pars_fragment>
#include <normal_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( 0.0, 0.0, 0.0, opacity );
	#include <clipping_planes_fragment>
	#include <logdepthbuf_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	gl_FragColor = vec4( normalize( normal ) * 0.5 + 0.5, diffuseColor.a );
	#ifdef OPAQUE
		gl_FragColor.a = 1.0;
	#endif
}`,N1=`#define PHONG
varying vec3 vViewPosition;
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <envmap_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <shadowmap_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphinstance_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	vViewPosition = - mvPosition.xyz;
	#include <worldpos_vertex>
	#include <envmap_vertex>
	#include <shadowmap_vertex>
	#include <fog_vertex>
}`,U1=`#define PHONG
uniform vec3 diffuse;
uniform vec3 emissive;
uniform vec3 specular;
uniform float shininess;
uniform float opacity;
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <aomap_pars_fragment>
#include <lightmap_pars_fragment>
#include <emissivemap_pars_fragment>
#include <cube_uv_reflection_fragment>
#include <envmap_common_pars_fragment>
#include <envmap_pars_fragment>
#include <envmap_physical_pars_fragment>
#include <fog_pars_fragment>
#include <bsdfs>
#include <lights_pars_begin>
#include <normal_pars_fragment>
#include <lights_phong_pars_fragment>
#include <shadowmap_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <specularmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	ReflectedLight reflectedLight = ReflectedLight( vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ) );
	vec3 totalEmissiveRadiance = emissive;
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <specularmap_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	#include <emissivemap_fragment>
	#include <lights_phong_fragment>
	#include <lights_fragment_begin>
	#include <lights_fragment_maps>
	#include <lights_fragment_end>
	#include <aomap_fragment>
	vec3 outgoingLight = reflectedLight.directDiffuse + reflectedLight.indirectDiffuse + reflectedLight.directSpecular + reflectedLight.indirectSpecular + totalEmissiveRadiance;
	#include <envmap_fragment>
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,F1=`#define STANDARD
varying vec3 vViewPosition;
#ifdef USE_TRANSMISSION
	varying vec3 vWorldPosition;
#endif
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <shadowmap_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	vViewPosition = - mvPosition.xyz;
	#include <worldpos_vertex>
	#include <shadowmap_vertex>
	#include <fog_vertex>
#ifdef USE_TRANSMISSION
	vWorldPosition = worldPosition.xyz;
#endif
}`,k1=`#define STANDARD
#ifdef PHYSICAL
	#define IOR
	#define USE_SPECULAR
#endif
uniform vec3 diffuse;
uniform vec3 emissive;
uniform float roughness;
uniform float metalness;
uniform float opacity;
#ifdef IOR
	uniform float ior;
#endif
#ifdef USE_SPECULAR
	uniform float specularIntensity;
	uniform vec3 specularColor;
	#ifdef USE_SPECULAR_COLORMAP
		uniform sampler2D specularColorMap;
	#endif
	#ifdef USE_SPECULAR_INTENSITYMAP
		uniform sampler2D specularIntensityMap;
	#endif
#endif
#ifdef USE_CLEARCOAT
	uniform float clearcoat;
	uniform float clearcoatRoughness;
#endif
#ifdef USE_DISPERSION
	uniform float dispersion;
#endif
#ifdef USE_RETROREFLECTION
	uniform float retroreflectivity;
#endif
#ifdef USE_IRIDESCENCE
	uniform float iridescence;
	uniform float iridescenceIOR;
	uniform float iridescenceThicknessMinimum;
	uniform float iridescenceThicknessMaximum;
#endif
#ifdef USE_SHEEN
	uniform vec3 sheenColor;
	uniform float sheenRoughness;
	#ifdef USE_SHEEN_COLORMAP
		uniform sampler2D sheenColorMap;
	#endif
	#ifdef USE_SHEEN_ROUGHNESSMAP
		uniform sampler2D sheenRoughnessMap;
	#endif
#endif
#ifdef USE_ANISOTROPY
	uniform vec2 anisotropyVector;
	#ifdef USE_ANISOTROPYMAP
		uniform sampler2D anisotropyMap;
	#endif
#endif
varying vec3 vViewPosition;
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <aomap_pars_fragment>
#include <lightmap_pars_fragment>
#include <emissivemap_pars_fragment>
#include <iridescence_fragment>
#include <cube_uv_reflection_fragment>
#include <envmap_common_pars_fragment>
#include <envmap_physical_pars_fragment>
#include <fog_pars_fragment>
#include <lights_pars_begin>
#include <normal_pars_fragment>
#include <lights_physical_pars_fragment>
#include <transmission_pars_fragment>
#include <shadowmap_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <clearcoat_pars_fragment>
#include <iridescence_pars_fragment>
#include <roughnessmap_pars_fragment>
#include <metalnessmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	ReflectedLight reflectedLight = ReflectedLight( vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ) );
	vec3 totalEmissiveRadiance = emissive;
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <roughnessmap_fragment>
	#include <metalnessmap_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	#include <clearcoat_normal_fragment_begin>
	#include <clearcoat_normal_fragment_maps>
	#include <emissivemap_fragment>
	#include <lights_physical_fragment>
	#include <lights_fragment_begin>
	#include <lights_fragment_maps>
	#include <lights_fragment_end>
	#include <aomap_fragment>
	vec3 totalDiffuse = reflectedLight.directDiffuse + reflectedLight.indirectDiffuse;
	vec3 totalSpecular = reflectedLight.directSpecular + reflectedLight.indirectSpecular;
	#include <transmission_fragment>
	vec3 outgoingLight = totalDiffuse + totalSpecular + totalEmissiveRadiance;
	#ifdef USE_SHEEN
 
		outgoingLight = outgoingLight + sheenSpecularDirect + sheenSpecularIndirect;
 
 	#endif
	#ifdef USE_CLEARCOAT
		float dotNVcc = saturate( dot( geometryClearcoatNormal, geometryViewDir ) );
		vec3 Fcc = F_Schlick( material.clearcoatF0, material.clearcoatF90, dotNVcc );
		outgoingLight = outgoingLight * ( 1.0 - material.clearcoat * Fcc ) + ( clearcoatSpecularDirect + clearcoatSpecularIndirect ) * material.clearcoat;
	#endif
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,B1=`#define TOON
varying vec3 vViewPosition;
#include <common>
#include <batching_pars_vertex>
#include <uv_pars_vertex>
#include <displacementmap_pars_vertex>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <normal_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <shadowmap_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <normal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <displacementmap_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	vViewPosition = - mvPosition.xyz;
	#include <worldpos_vertex>
	#include <shadowmap_vertex>
	#include <fog_vertex>
}`,z1=`#define TOON
uniform vec3 diffuse;
uniform vec3 emissive;
uniform float opacity;
#include <common>
#include <dithering_pars_fragment>
#include <color_pars_fragment>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <aomap_pars_fragment>
#include <lightmap_pars_fragment>
#include <emissivemap_pars_fragment>
#include <gradientmap_pars_fragment>
#include <fog_pars_fragment>
#include <bsdfs>
#include <lights_pars_begin>
#include <normal_pars_fragment>
#include <lights_toon_pars_fragment>
#include <shadowmap_pars_fragment>
#include <bumpmap_pars_fragment>
#include <normalmap_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	ReflectedLight reflectedLight = ReflectedLight( vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ), vec3( 0.0 ) );
	vec3 totalEmissiveRadiance = emissive;
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <color_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	#include <normal_fragment_begin>
	#include <normal_fragment_maps>
	#include <emissivemap_fragment>
	#include <lights_toon_fragment>
	#include <lights_fragment_begin>
	#include <lights_fragment_maps>
	#include <lights_fragment_end>
	#include <aomap_fragment>
	vec3 outgoingLight = reflectedLight.directDiffuse + reflectedLight.indirectDiffuse + totalEmissiveRadiance;
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
	#include <dithering_fragment>
}`,V1=`uniform float size;
uniform float scale;
#include <common>
#include <color_pars_vertex>
#include <fog_pars_vertex>
#include <morphtarget_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
#ifdef USE_POINTS_UV
	varying vec2 vUv;
	uniform mat3 uvTransform;
#endif
void main() {
	#ifdef USE_POINTS_UV
		vUv = ( uvTransform * vec3( uv, 1 ) ).xy;
	#endif
	#include <color_vertex>
	#include <morphinstance_vertex>
	#include <morphcolor_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <project_vertex>
	gl_PointSize = size;
	#ifdef USE_SIZEATTENUATION
		bool isPerspective = isPerspectiveMatrix( projectionMatrix );
		if ( isPerspective ) gl_PointSize *= ( scale / - mvPosition.z );
	#endif
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	#include <worldpos_vertex>
	#include <fog_vertex>
}`,G1=`uniform vec3 diffuse;
uniform float opacity;
#include <common>
#include <color_pars_fragment>
#include <map_particle_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <fog_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	vec3 outgoingLight = vec3( 0.0 );
	#include <logdepthbuf_fragment>
	#include <map_particle_fragment>
	#include <color_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	outgoingLight = diffuseColor.rgb;
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
}`,H1=`#include <common>
#include <batching_pars_vertex>
#include <fog_pars_vertex>
#include <morphtarget_pars_vertex>
#include <skinning_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <shadowmap_pars_vertex>
void main() {
	#include <batching_vertex>
	#include <beginnormal_vertex>
	#include <morphinstance_vertex>
	#include <morphnormal_vertex>
	#include <skinbase_vertex>
	#include <skinnormal_vertex>
	#include <defaultnormal_vertex>
	#include <begin_vertex>
	#include <morphtarget_vertex>
	#include <skinning_vertex>
	#include <project_vertex>
	#include <logdepthbuf_vertex>
	#include <worldpos_vertex>
	#include <shadowmap_vertex>
	#include <fog_vertex>
}`,W1=`uniform vec3 color;
uniform float opacity;
#include <common>
#include <fog_pars_fragment>
#include <bsdfs>
#include <lights_pars_begin>
#include <logdepthbuf_pars_fragment>
#include <shadowmap_pars_fragment>
#include <shadowmask_pars_fragment>
void main() {
	#include <logdepthbuf_fragment>
	gl_FragColor = vec4( color, opacity * ( 1.0 - getShadowMask() ) );
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
	#include <premultiplied_alpha_fragment>
}`,X1=`uniform float rotation;
uniform vec2 center;
#include <common>
#include <uv_pars_vertex>
#include <fog_pars_vertex>
#include <logdepthbuf_pars_vertex>
#include <clipping_planes_pars_vertex>
void main() {
	#include <uv_vertex>
	vec4 mvPosition = modelViewMatrix[ 3 ];
	vec2 scale = vec2( length( modelMatrix[ 0 ].xyz ), length( modelMatrix[ 1 ].xyz ) );
	#ifndef USE_SIZEATTENUATION
		bool isPerspective = isPerspectiveMatrix( projectionMatrix );
		if ( isPerspective ) scale *= - mvPosition.z;
	#endif
	vec2 alignedPosition = ( position.xy - ( center - vec2( 0.5 ) ) ) * scale;
	vec2 rotatedPosition;
	rotatedPosition.x = cos( rotation ) * alignedPosition.x - sin( rotation ) * alignedPosition.y;
	rotatedPosition.y = sin( rotation ) * alignedPosition.x + cos( rotation ) * alignedPosition.y;
	mvPosition.xy += rotatedPosition;
	gl_Position = projectionMatrix * mvPosition;
	#include <logdepthbuf_vertex>
	#include <clipping_planes_vertex>
	#include <fog_vertex>
}`,q1=`uniform vec3 diffuse;
uniform float opacity;
#include <common>
#include <uv_pars_fragment>
#include <map_pars_fragment>
#include <alphamap_pars_fragment>
#include <alphatest_pars_fragment>
#include <alphahash_pars_fragment>
#include <fog_pars_fragment>
#include <logdepthbuf_pars_fragment>
#include <clipping_planes_pars_fragment>
void main() {
	vec4 diffuseColor = vec4( diffuse, opacity );
	#include <clipping_planes_fragment>
	vec3 outgoingLight = vec3( 0.0 );
	#include <logdepthbuf_fragment>
	#include <map_fragment>
	#include <alphamap_fragment>
	#include <alphatest_fragment>
	#include <alphahash_fragment>
	outgoingLight = diffuseColor.rgb;
	#include <opaque_fragment>
	#include <tonemapping_fragment>
	#include <colorspace_fragment>
	#include <fog_fragment>
}`,Ye={alphahash_fragment:dw,alphahash_pars_fragment:pw,alphamap_fragment:mw,alphamap_pars_fragment:gw,alphatest_fragment:_w,alphatest_pars_fragment:vw,aomap_fragment:yw,aomap_pars_fragment:xw,batching_pars_vertex:bw,batching_vertex:Sw,begin_vertex:ww,beginnormal_vertex:Mw,bsdfs:Ew,iridescence_fragment:Aw,bumpmap_pars_fragment:Tw,clipping_planes_fragment:Cw,clipping_planes_pars_fragment:Rw,clipping_planes_pars_vertex:Pw,clipping_planes_vertex:Iw,color_fragment:Lw,color_pars_fragment:Dw,color_pars_vertex:Ow,color_vertex:Nw,common:Uw,cube_uv_reflection_fragment:Fw,defaultnormal_vertex:kw,displacementmap_pars_vertex:Bw,displacementmap_vertex:zw,emissivemap_fragment:Vw,emissivemap_pars_fragment:Gw,colorspace_fragment:Hw,colorspace_pars_fragment:Ww,envmap_fragment:Xw,envmap_common_pars_fragment:qw,envmap_pars_fragment:jw,envmap_pars_vertex:Yw,envmap_physical_pars_fragment:sM,envmap_vertex:$w,fog_vertex:Zw,fog_pars_vertex:Kw,fog_fragment:Jw,fog_pars_fragment:Qw,gradientmap_pars_fragment:eM,lightmap_pars_fragment:tM,lights_lambert_fragment:nM,lights_lambert_pars_fragment:iM,lights_pars_begin:rM,lights_toon_fragment:oM,lights_toon_pars_fragment:aM,lights_phong_fragment:lM,lights_phong_pars_fragment:cM,lights_physical_fragment:uM,lights_physical_pars_fragment:hM,lights_fragment_begin:fM,lights_fragment_maps:dM,lights_fragment_end:pM,lightprobes_pars_fragment:mM,logdepthbuf_fragment:gM,logdepthbuf_pars_fragment:_M,logdepthbuf_pars_vertex:vM,logdepthbuf_vertex:yM,map_fragment:xM,map_pars_fragment:bM,map_particle_fragment:SM,map_particle_pars_fragment:wM,metalnessmap_fragment:MM,metalnessmap_pars_fragment:EM,morphinstance_vertex:AM,morphcolor_vertex:TM,morphnormal_vertex:CM,morphtarget_pars_vertex:RM,morphtarget_vertex:PM,normal_fragment_begin:IM,normal_fragment_maps:LM,normal_pars_fragment:DM,normal_pars_vertex:OM,normal_vertex:NM,normalmap_pars_fragment:UM,clearcoat_normal_fragment_begin:FM,clearcoat_normal_fragment_maps:kM,clearcoat_pars_fragment:BM,iridescence_pars_fragment:zM,opaque_fragment:VM,packing:GM,premultiplied_alpha_fragment:HM,project_vertex:WM,dithering_fragment:XM,dithering_pars_fragment:qM,roughnessmap_fragment:jM,roughnessmap_pars_fragment:YM,shadowmap_pars_fragment:$M,shadowmap_pars_vertex:ZM,shadowmap_vertex:KM,shadowmask_pars_fragment:JM,skinbase_vertex:QM,skinning_pars_vertex:e1,skinning_vertex:t1,skinnormal_vertex:n1,specularmap_fragment:i1,specularmap_pars_fragment:r1,tonemapping_fragment:s1,tonemapping_pars_fragment:o1,transmission_fragment:a1,transmission_pars_fragment:l1,uv_pars_fragment:c1,uv_pars_vertex:u1,uv_vertex:h1,worldpos_vertex:f1,background_vert:d1,background_frag:p1,backgroundCube_vert:m1,backgroundCube_frag:g1,cube_vert:_1,cube_frag:v1,depth_vert:y1,depth_frag:x1,distance_vert:b1,distance_frag:S1,equirect_vert:w1,equirect_frag:M1,linedashed_vert:E1,linedashed_frag:A1,meshbasic_vert:T1,meshbasic_frag:C1,meshlambert_vert:R1,meshlambert_frag:P1,meshmatcap_vert:I1,meshmatcap_frag:L1,meshnormal_vert:D1,meshnormal_frag:O1,meshphong_vert:N1,meshphong_frag:U1,meshphysical_vert:F1,meshphysical_frag:k1,meshtoon_vert:B1,meshtoon_frag:z1,points_vert:V1,points_frag:G1,shadow_vert:H1,shadow_frag:W1,sprite_vert:X1,sprite_frag:q1},ye={common:{diffuse:{value:new We(16777215)},opacity:{value:1},map:{value:null},mapTransform:{value:new He},alphaMap:{value:null},alphaMapTransform:{value:new He},alphaTest:{value:0}},specularmap:{specularMap:{value:null},specularMapTransform:{value:new He}},envmap:{envMap:{value:null},envMapRotation:{value:new He},reflectivity:{value:1},ior:{value:1.5},refractionRatio:{value:.98},dfgLUT:{value:null}},aomap:{aoMap:{value:null},aoMapIntensity:{value:1},aoMapTransform:{value:new He}},lightmap:{lightMap:{value:null},lightMapIntensity:{value:1},lightMapTransform:{value:new He}},bumpmap:{bumpMap:{value:null},bumpMapTransform:{value:new He},bumpScale:{value:1}},normalmap:{normalMap:{value:null},normalMapTransform:{value:new He},normalScale:{value:new fe(1,1)}},displacementmap:{displacementMap:{value:null},displacementMapTransform:{value:new He},displacementScale:{value:1},displacementBias:{value:0}},emissivemap:{emissiveMap:{value:null},emissiveMapTransform:{value:new He}},metalnessmap:{metalnessMap:{value:null},metalnessMapTransform:{value:new He}},roughnessmap:{roughnessMap:{value:null},roughnessMapTransform:{value:new He}},gradientmap:{gradientMap:{value:null}},fog:{fogDensity:{value:25e-5},fogNear:{value:1},fogFar:{value:2e3},fogColor:{value:new We(16777215)}},lights:{ambientLightColor:{value:[]},lightProbe:{value:[]},sunLights:{value:[],properties:{direction:{},color:{}}},sunLightShadows:{value:[],properties:{shadowIntensity:1,shadowBias:{},shadowNormalBias:{},shadowRadius:{},shadowMapSize:{}}},sunShadowMatrix:{value:[]},sunShadowCascade:{value:[]},directionalLights:{value:[],properties:{direction:{},color:{}}},directionalLightShadows:{value:[],properties:{shadowIntensity:1,shadowBias:{},shadowNormalBias:{},shadowRadius:{},shadowMapSize:{}}},directionalShadowMatrix:{value:[]},spotLights:{value:[],properties:{color:{},position:{},direction:{},distance:{},coneCos:{},penumbraCos:{},decay:{}}},spotLightShadows:{value:[],properties:{shadowIntensity:1,shadowBias:{},shadowNormalBias:{},shadowRadius:{},shadowMapSize:{}}},spotLightMap:{value:[]},spotLightMatrix:{value:[]},pointLights:{value:[],properties:{color:{},position:{},decay:{},distance:{}}},pointLightShadows:{value:[],properties:{shadowIntensity:1,shadowBias:{},shadowNormalBias:{},shadowRadius:{},shadowMapSize:{},shadowCameraNear:{},shadowCameraFar:{}}},pointShadowMatrix:{value:[]},hemisphereLights:{value:[],properties:{direction:{},skyColor:{},groundColor:{}}},rectAreaLights:{value:[],properties:{color:{},position:{},width:{},height:{}}},ltc_1:{value:null},ltc_2:{value:null},probesSH:{value:null},probesMin:{value:new F},probesMax:{value:new F},probesResolution:{value:new F}},points:{diffuse:{value:new We(16777215)},opacity:{value:1},size:{value:1},scale:{value:1},map:{value:null},alphaMap:{value:null},alphaMapTransform:{value:new He},alphaTest:{value:0},uvTransform:{value:new He}},sprite:{diffuse:{value:new We(16777215)},opacity:{value:1},center:{value:new fe(.5,.5)},rotation:{value:0},map:{value:null},mapTransform:{value:new He},alphaMap:{value:null},alphaMapTransform:{value:new He},alphaTest:{value:0}}},mi={basic:{uniforms:Qt([ye.common,ye.specularmap,ye.envmap,ye.aomap,ye.lightmap,ye.fog]),vertexShader:Ye.meshbasic_vert,fragmentShader:Ye.meshbasic_frag},lambert:{uniforms:Qt([ye.common,ye.specularmap,ye.envmap,ye.aomap,ye.lightmap,ye.emissivemap,ye.bumpmap,ye.normalmap,ye.displacementmap,ye.fog,ye.lights,{emissive:{value:new We(0)},envMapIntensity:{value:1}}]),vertexShader:Ye.meshlambert_vert,fragmentShader:Ye.meshlambert_frag},phong:{uniforms:Qt([ye.common,ye.specularmap,ye.envmap,ye.aomap,ye.lightmap,ye.emissivemap,ye.bumpmap,ye.normalmap,ye.displacementmap,ye.fog,ye.lights,{emissive:{value:new We(0)},specular:{value:new We(1118481)},shininess:{value:30},envMapIntensity:{value:1}}]),vertexShader:Ye.meshphong_vert,fragmentShader:Ye.meshphong_frag},standard:{uniforms:Qt([ye.common,ye.envmap,ye.aomap,ye.lightmap,ye.emissivemap,ye.bumpmap,ye.normalmap,ye.displacementmap,ye.roughnessmap,ye.metalnessmap,ye.fog,ye.lights,{emissive:{value:new We(0)},roughness:{value:1},metalness:{value:0},envMapIntensity:{value:1}}]),vertexShader:Ye.meshphysical_vert,fragmentShader:Ye.meshphysical_frag},toon:{uniforms:Qt([ye.common,ye.aomap,ye.lightmap,ye.emissivemap,ye.bumpmap,ye.normalmap,ye.displacementmap,ye.gradientmap,ye.fog,ye.lights,{emissive:{value:new We(0)}}]),vertexShader:Ye.meshtoon_vert,fragmentShader:Ye.meshtoon_frag},matcap:{uniforms:Qt([ye.common,ye.bumpmap,ye.normalmap,ye.displacementmap,ye.fog,{matcap:{value:null}}]),vertexShader:Ye.meshmatcap_vert,fragmentShader:Ye.meshmatcap_frag},points:{uniforms:Qt([ye.points,ye.fog]),vertexShader:Ye.points_vert,fragmentShader:Ye.points_frag},dashed:{uniforms:Qt([ye.common,ye.fog,{scale:{value:1},dashSize:{value:1},totalSize:{value:2}}]),vertexShader:Ye.linedashed_vert,fragmentShader:Ye.linedashed_frag},depth:{uniforms:Qt([ye.common,ye.displacementmap]),vertexShader:Ye.depth_vert,fragmentShader:Ye.depth_frag},normal:{uniforms:Qt([ye.common,ye.bumpmap,ye.normalmap,ye.displacementmap,{opacity:{value:1}}]),vertexShader:Ye.meshnormal_vert,fragmentShader:Ye.meshnormal_frag},sprite:{uniforms:Qt([ye.sprite,ye.fog]),vertexShader:Ye.sprite_vert,fragmentShader:Ye.sprite_frag},background:{uniforms:{uvTransform:{value:new He},t2D:{value:null},backgroundIntensity:{value:1}},vertexShader:Ye.background_vert,fragmentShader:Ye.background_frag},backgroundCube:{uniforms:{envMap:{value:null},backgroundBlurriness:{value:0},backgroundIntensity:{value:1},backgroundRotation:{value:new He}},vertexShader:Ye.backgroundCube_vert,fragmentShader:Ye.backgroundCube_frag},cube:{uniforms:{tCube:{value:null},tFlip:{value:-1},opacity:{value:1}},vertexShader:Ye.cube_vert,fragmentShader:Ye.cube_frag},equirect:{uniforms:{tEquirect:{value:null}},vertexShader:Ye.equirect_vert,fragmentShader:Ye.equirect_frag},distance:{uniforms:Qt([ye.common,ye.displacementmap,{referencePosition:{value:new F},nearDistance:{value:1},farDistance:{value:1e3}}]),vertexShader:Ye.distance_vert,fragmentShader:Ye.distance_frag},shadow:{uniforms:Qt([ye.lights,ye.fog,{color:{value:new We(0)},opacity:{value:1}}]),vertexShader:Ye.shadow_vert,fragmentShader:Ye.shadow_frag}};mi.physical={uniforms:Qt([mi.standard.uniforms,{clearcoat:{value:0},clearcoatMap:{value:null},clearcoatMapTransform:{value:new He},clearcoatNormalMap:{value:null},clearcoatNormalMapTransform:{value:new He},clearcoatNormalScale:{value:new fe(1,1)},clearcoatRoughness:{value:0},clearcoatRoughnessMap:{value:null},clearcoatRoughnessMapTransform:{value:new He},dispersion:{value:0},retroreflectivity:{value:0},iridescence:{value:0},iridescenceMap:{value:null},iridescenceMapTransform:{value:new He},iridescenceIOR:{value:1.3},iridescenceThicknessMinimum:{value:100},iridescenceThicknessMaximum:{value:400},iridescenceThicknessMap:{value:null},iridescenceThicknessMapTransform:{value:new He},sheen:{value:0},sheenColor:{value:new We(0)},sheenColorMap:{value:null},sheenColorMapTransform:{value:new He},sheenRoughness:{value:1},sheenRoughnessMap:{value:null},sheenRoughnessMapTransform:{value:new He},transmission:{value:0},transmissionMap:{value:null},transmissionMapTransform:{value:new He},transmissionSamplerSize:{value:new fe},transmissionSamplerMap:{value:null},thickness:{value:0},thicknessMap:{value:null},thicknessMapTransform:{value:new He},attenuationDistance:{value:0},attenuationColor:{value:new We(0)},specularColor:{value:new We(1,1,1)},specularColorMap:{value:null},specularColorMapTransform:{value:new He},specularIntensity:{value:1},specularIntensityMap:{value:null},specularIntensityMapTransform:{value:new He},anisotropyVector:{value:new fe},anisotropyMap:{value:null},anisotropyMapTransform:{value:new He}}]),vertexShader:Ye.meshphysical_vert,fragmentShader:Ye.meshphysical_frag};var tu={r:0,b:0,g:0},j1=new st,Kg=new He;Kg.set(-1,0,0,0,1,0,0,0,1);function Y1(i,e,n,r,s,o){let a=new We(0),l=s===!0?0:1,c,u,h=null,d=0,f=null;function p(w){let T=w.isScene===!0?w.background:null;if(T&&T.isTexture){let y=w.backgroundBlurriness>0;T=e.get(T,y)}return T}function g(w){let T=!1,y=p(w);y===null?_(a,l):y&&y.isColor&&(_(y,1),T=!0);let b=i.xr.getEnvironmentBlendMode();b==="additive"?n.buffers.color.setClear(0,0,0,1,o):b==="alpha-blend"&&n.buffers.color.setClear(0,0,0,0,o),(i.autoClear||T)&&(n.buffers.depth.setTest(!0),n.buffers.depth.setMask(!0),n.buffers.color.setMask(!0),i.clear(i.autoClearColor,i.autoClearDepth,i.autoClearStencil))}function v(w,T){let y=p(T);y&&(y.isCubeTexture||y.mapping===Wo)?(u===void 0&&(u=new kt(new Ls(1,1,1),new Jt({name:"BackgroundCubeMaterial",uniforms:kr(mi.backgroundCube.uniforms),vertexShader:mi.backgroundCube.vertexShader,fragmentShader:mi.backgroundCube.fragmentShader,side:Gt,depthTest:!1,depthWrite:!1,fog:!1,allowOverride:!1})),u.geometry.deleteAttribute("normal"),u.geometry.deleteAttribute("uv"),u.onBeforeRender=function(b,S,A){this.matrixWorld.copyPosition(A.matrixWorld)},Object.defineProperty(u.material,"envMap",{get:function(){return this.uniforms.envMap.value}}),r.update(u)),u.material.uniforms.envMap.value=y,u.material.uniforms.backgroundBlurriness.value=T.backgroundBlurriness,u.material.uniforms.backgroundIntensity.value=T.backgroundIntensity,u.material.uniforms.backgroundRotation.value.setFromMatrix4(j1.makeRotationFromEuler(T.backgroundRotation)).transpose(),y.isCubeTexture&&y.isRenderTargetTexture===!1&&u.material.uniforms.backgroundRotation.value.premultiply(Kg),u.material.toneMapped=Ke.getTransfer(y.colorSpace)!==rt,(h!==y||d!==y.version||f!==i.toneMapping)&&(u.material.needsUpdate=!0,h=y,d=y.version,f=i.toneMapping),u.layers.enableAll(),w.unshift(u,u.geometry,u.material,0,0,null)):y&&y.isTexture&&(c===void 0&&(c=new kt(new Oo(2,2),new Jt({name:"BackgroundMaterial",uniforms:kr(mi.background.uniforms),vertexShader:mi.background.vertexShader,fragmentShader:mi.background.fragmentShader,side:ir,depthTest:!1,depthWrite:!1,fog:!1,allowOverride:!1})),c.geometry.deleteAttribute("normal"),Object.defineProperty(c.material,"map",{get:function(){return this.uniforms.t2D.value}}),r.update(c)),c.material.uniforms.t2D.value=y,c.material.uniforms.backgroundIntensity.value=T.backgroundIntensity,c.material.toneMapped=Ke.getTransfer(y.colorSpace)!==rt,y.matrixAutoUpdate===!0&&y.updateMatrix(),c.material.uniforms.uvTransform.value.copy(y.matrix),(h!==y||d!==y.version||f!==i.toneMapping)&&(c.material.needsUpdate=!0,h=y,d=y.version,f=i.toneMapping),c.layers.enableAll(),w.unshift(c,c.geometry,c.material,0,0,null))}function _(w,T){w.getRGB(tu,Gf(i)),n.buffers.color.setClear(tu.r,tu.g,tu.b,T,o)}function m(){u!==void 0&&(u.geometry.dispose(),u.material.dispose(),u=void 0),c!==void 0&&(c.geometry.dispose(),c.material.dispose(),c=void 0)}return{getClearColor:function(){return a},setClearColor:function(w,T=1){a.set(w),l=T,_(a,l)},getClearAlpha:function(){return l},setClearAlpha:function(w){l=w,_(a,l)},render:g,addToRenderList:v,dispose:m}}function $1(i,e){let n=i.getParameter(i.MAX_VERTEX_ATTRIBS),r={},s=f(null),o=s,a=!1;function l(P,O,U,M,I){let D=!1,k=d(P,M,U,O);o!==k&&(o=k,u(o.object)),D=p(P,M,U,I),D&&g(P,M,U,I),I!==null&&e.update(I,i.ELEMENT_ARRAY_BUFFER),(D||a)&&(a=!1,y(P,O,U,M),I!==null&&i.bindBuffer(i.ELEMENT_ARRAY_BUFFER,e.get(I).buffer))}function c(){return i.createVertexArray()}function u(P){return i.bindVertexArray(P)}function h(P){return i.deleteVertexArray(P)}function d(P,O,U,M){let I=M.wireframe===!0,D=r[O.id];D===void 0&&(D={},r[O.id]=D);let k=P.isInstancedMesh===!0?P.id:0,q=D[k];q===void 0&&(q={},D[k]=q);let W=q[U.id];W===void 0&&(W={},q[U.id]=W);let j=W[I];return j===void 0&&(j=f(c()),W[I]=j),j}function f(P){let O=[],U=[],M=[];for(let I=0;I<n;I++)O[I]=0,U[I]=0,M[I]=0;return{geometry:null,program:null,wireframe:!1,newAttributes:O,enabledAttributes:U,attributeDivisors:M,object:P,attributes:{},index:null}}function p(P,O,U,M){let I=o.attributes,D=O.attributes,k=0,q=U.getAttributes();for(let W in q)if(q[W].location>=0){let B=I[W],Q=D[W];if(Q===void 0&&(W==="instanceMatrix"&&P.instanceMatrix&&(Q=P.instanceMatrix),W==="instanceColor"&&P.instanceColor&&(Q=P.instanceColor)),B===void 0||B.attribute!==Q||Q&&B.data!==Q.data)return!0;k++}return o.attributesNum!==k||o.index!==M}function g(P,O,U,M){let I={},D=O.attributes,k=0,q=U.getAttributes();for(let W in q)if(q[W].location>=0){let B=D[W];B===void 0&&(W==="instanceMatrix"&&P.instanceMatrix&&(B=P.instanceMatrix),W==="instanceColor"&&P.instanceColor&&(B=P.instanceColor));let Q={};Q.attribute=B,B&&B.data&&(Q.data=B.data),I[W]=Q,k++}o.attributes=I,o.attributesNum=k,o.index=M}function v(){let P=o.newAttributes;for(let O=0,U=P.length;O<U;O++)P[O]=0}function _(P){m(P,0)}function m(P,O){let U=o.newAttributes,M=o.enabledAttributes,I=o.attributeDivisors;U[P]=1,M[P]===0&&(i.enableVertexAttribArray(P),M[P]=1),I[P]!==O&&(i.vertexAttribDivisor(P,O),I[P]=O)}function w(){let P=o.newAttributes,O=o.enabledAttributes;for(let U=0,M=O.length;U<M;U++)O[U]!==P[U]&&(i.disableVertexAttribArray(U),O[U]=0)}function T(P,O,U,M,I,D,k){k===!0?i.vertexAttribIPointer(P,O,U,I,D):i.vertexAttribPointer(P,O,U,M,I,D)}function y(P,O,U,M){v();let I=M.attributes,D=U.getAttributes(),k=O.defaultAttributeValues;for(let q in D){let W=D[q];if(W.location>=0){let j=I[q];if(j===void 0&&(q==="instanceMatrix"&&P.instanceMatrix&&(j=P.instanceMatrix),q==="instanceColor"&&P.instanceColor&&(j=P.instanceColor)),j!==void 0){let B=j.normalized,Q=j.itemSize,re=e.get(j);if(re===void 0)continue;let be=re.buffer,ee=re.type,oe=re.bytesPerElement,V=ee===i.INT||ee===i.UNSIGNED_INT||j.gpuType===mc;if(j.isInterleavedBufferAttribute){let Z=j.data,ce=Z.stride,Ee=j.offset;if(Z.isInstancedInterleavedBuffer){for(let ue=0;ue<W.locationSize;ue++)m(W.location+ue,Z.meshPerAttribute);P.isInstancedMesh!==!0&&M._maxInstanceCount===void 0&&(M._maxInstanceCount=Z.meshPerAttribute*Z.count)}else for(let ue=0;ue<W.locationSize;ue++)_(W.location+ue);i.bindBuffer(i.ARRAY_BUFFER,be);for(let ue=0;ue<W.locationSize;ue++)T(W.location+ue,Q/W.locationSize,ee,B,ce*oe,(Ee+Q/W.locationSize*ue)*oe,V)}else{if(j.isInstancedBufferAttribute){for(let Z=0;Z<W.locationSize;Z++)m(W.location+Z,j.meshPerAttribute);P.isInstancedMesh!==!0&&M._maxInstanceCount===void 0&&(M._maxInstanceCount=j.meshPerAttribute*j.count)}else for(let Z=0;Z<W.locationSize;Z++)_(W.location+Z);i.bindBuffer(i.ARRAY_BUFFER,be);for(let Z=0;Z<W.locationSize;Z++)T(W.location+Z,Q/W.locationSize,ee,B,Q*oe,Q/W.locationSize*Z*oe,V)}}else if(k!==void 0){let B=k[q];if(B!==void 0)switch(B.length){case 2:i.vertexAttrib2fv(W.location,B);break;case 3:i.vertexAttrib3fv(W.location,B);break;case 4:i.vertexAttrib4fv(W.location,B);break;default:i.vertexAttrib1fv(W.location,B)}}}}w()}function b(){C();for(let P in r){let O=r[P];for(let U in O){let M=O[U];for(let I in M){let D=M[I];for(let k in D)h(D[k].object),delete D[k];delete M[I]}}delete r[P]}}function S(P){if(r[P.id]===void 0)return;let O=r[P.id];for(let U in O){let M=O[U];for(let I in M){let D=M[I];for(let k in D)h(D[k].object),delete D[k];delete M[I]}}delete r[P.id]}function A(P){for(let O in r){let U=r[O];for(let M in U){let I=U[M];if(I[P.id]===void 0)continue;let D=I[P.id];for(let k in D)h(D[k].object),delete D[k];delete I[P.id]}}}function x(P){for(let O in r){let U=r[O],M=P.isInstancedMesh===!0?P.id:0,I=U[M];if(I!==void 0){for(let D in I){let k=I[D];for(let q in k)h(k[q].object),delete k[q];delete I[D]}delete U[M],Object.keys(U).length===0&&delete r[O]}}}function C(){L(),a=!0,o!==s&&(o=s,u(o.object))}function L(){s.geometry=null,s.program=null,s.wireframe=!1}return{setup:l,reset:C,resetDefaultState:L,dispose:b,releaseStatesOfGeometry:S,releaseStatesOfObject:x,releaseStatesOfProgram:A,initAttributes:v,enableAttribute:_,disableUnusedAttributes:w}}function Z1(i,e,n){let r;function s(c){r=c}function o(c,u){i.drawArrays(r,c,u),n.update(u,r,1)}function a(c,u,h){h!==0&&(i.drawArraysInstanced(r,c,u,h),n.update(u,r,h))}function l(c,u,h){if(h===0)return;e.get("WEBGL_multi_draw").multiDrawArraysWEBGL(r,c,0,u,0,h);let f=0;for(let p=0;p<h;p++)f+=u[p];n.update(f,r,1)}this.setMode=s,this.render=o,this.renderInstances=a,this.renderMultiDraw=l}function K1(i,e,n,r){let s;function o(){if(s!==void 0)return s;if(e.has("EXT_texture_filter_anisotropic")===!0){let A=e.get("EXT_texture_filter_anisotropic");s=i.getParameter(A.MAX_TEXTURE_MAX_ANISOTROPY_EXT)}else s=0;return s}function a(A){return!(A!==Nn&&r.convert(A)!==i.getParameter(i.IMPLEMENTATION_COLOR_READ_FORMAT))}function l(A){let x=A===En&&(e.has("EXT_color_buffer_half_float")||e.has("EXT_color_buffer_float"));return!(A!==mn&&A!==Jn&&!x&&r.convert(A)!==i.getParameter(i.IMPLEMENTATION_COLOR_READ_TYPE))}function c(A){if(A==="highp"){if(i.getShaderPrecisionFormat(i.VERTEX_SHADER,i.HIGH_FLOAT).precision>0&&i.getShaderPrecisionFormat(i.FRAGMENT_SHADER,i.HIGH_FLOAT).precision>0)return"highp";A="mediump"}return A==="mediump"&&i.getShaderPrecisionFormat(i.VERTEX_SHADER,i.MEDIUM_FLOAT).precision>0&&i.getShaderPrecisionFormat(i.FRAGMENT_SHADER,i.MEDIUM_FLOAT).precision>0?"mediump":"lowp"}let u=n.precision!==void 0?n.precision:"highp",h=c(u);h!==u&&(Ue("WebGLRenderer:",u,"not supported, using",h,"instead."),u=h);let d=n.logarithmicDepthBuffer===!0,f=n.reversedDepthBuffer===!0&&e.has("EXT_clip_control");n.reversedDepthBuffer===!0&&f===!1&&Ue("WebGLRenderer: Unable to use reversed depth buffer due to missing EXT_clip_control extension. Fallback to default depth buffer.");let p=i.getParameter(i.MAX_TEXTURE_IMAGE_UNITS),g=i.getParameter(i.MAX_VERTEX_TEXTURE_IMAGE_UNITS),v=i.getParameter(i.MAX_TEXTURE_SIZE),_=i.getParameter(i.MAX_CUBE_MAP_TEXTURE_SIZE),m=i.getParameter(i.MAX_VERTEX_ATTRIBS),w=i.getParameter(i.MAX_VERTEX_UNIFORM_VECTORS),T=i.getParameter(i.MAX_VARYING_VECTORS),y=i.getParameter(i.MAX_FRAGMENT_UNIFORM_VECTORS),b=i.getParameter(i.MAX_SAMPLES),S=i.getParameter(i.SAMPLES);return{isWebGL2:!0,getMaxAnisotropy:o,getMaxPrecision:c,textureFormatReadable:a,textureTypeReadable:l,precision:u,logarithmicDepthBuffer:d,reversedDepthBuffer:f,maxTextures:p,maxVertexTextures:g,maxTextureSize:v,maxCubemapSize:_,maxAttributes:m,maxVertexUniforms:w,maxVaryings:T,maxFragmentUniforms:y,maxSamples:b,samples:S}}function J1(i){let e=this,n=null,r=0,s=!1,o=!1,a=new an,l=new He,c={value:null,needsUpdate:!1};this.uniform=c,this.numPlanes=0,this.numIntersection=0,this.init=function(d,f){let p=d.length!==0||f||r!==0||s;return s=f,r=d.length,p},this.beginShadows=function(){o=!0,h(null)},this.endShadows=function(){o=!1},this.setGlobalState=function(d,f){n=h(d,f,0)},this.setState=function(d,f,p){let g=d.clippingPlanes,v=d.clipIntersection,_=d.clipShadows,m=i.get(d);if(!s||g===null||g.length===0||o&&!_)o?h(null):u();else{let w=o?0:r,T=w*4,y=m.clippingState||null;c.value=y,y=h(g,f,T,p);for(let b=0;b!==T;++b)y[b]=n[b];m.clippingState=y,this.numIntersection=v?this.numPlanes:0,this.numPlanes+=w}};function u(){c.value!==n&&(c.value=n,c.needsUpdate=r>0),e.numPlanes=r,e.numIntersection=0}function h(d,f,p,g){let v=d!==null?d.length:0,_=null;if(v!==0){if(_=c.value,g!==!0||_===null){let m=p+v*4,w=f.matrixWorldInverse;l.getNormalMatrix(w),(_===null||_.length<m)&&(_=new Float32Array(m));for(let T=0,y=p;T!==v;++T,y+=4)a.copy(d[T]).applyMatrix4(w,l),a.normal.toArray(_,y),_[y+3]=a.constant}c.value=_,c.needsUpdate=!0}return e.numPlanes=v,e.numIntersection=0,_}}var Ws=4,Q1=6,eE=20,tE=256,Jo=new nr,Rg=new We,Kf=null,Jf=0,Qf=0,ed=!1,nE=new F,Br=new F,iu=class{constructor(e){this._renderer=e,this._pingPongRenderTarget=null,this._lodMax=0,this._cubeSize=0,this._sizeLods=[],this._lodMeshes=[],this._backgroundBox=null,this._cubemapMaterial=null,this._equirectMaterial=null,this._blurMaterial=null,this._ggxMaterial=null}fromScene(e,n=0,r=.1,s=100,o={}){let{size:a=256,position:l=nE}=o;Kf=this._renderer.getRenderTarget(),Jf=this._renderer.getActiveCubeFace(),Qf=this._renderer.getActiveMipmapLevel(),ed=this._renderer.xr.enabled,this._renderer.xr.enabled=!1,this._setSize(a);let c=this._allocateTargets();return c.depthBuffer=!0,this._sceneToCubeUV(e,r,s,c,l),n>0&&this._blur(c,0,0,n),this._applyPMREM(c),this._cleanup(c),c}fromEquirectangular(e,n=null){return this._fromTexture(e,n)}fromCubemap(e,n=null){return this._fromTexture(e,n)}compileCubemapShader(){this._cubemapMaterial===null&&(this._cubemapMaterial=Lg(),this._compileMaterial(this._cubemapMaterial))}compileEquirectangularShader(){this._equirectMaterial===null&&(this._equirectMaterial=Ig(),this._compileMaterial(this._equirectMaterial))}dispose(){this._dispose(),this._cubemapMaterial!==null&&this._cubemapMaterial.dispose(),this._equirectMaterial!==null&&this._equirectMaterial.dispose(),this._backgroundBox!==null&&(this._backgroundBox.geometry.dispose(),this._backgroundBox.material.dispose())}_setSize(e){this._lodMax=Math.floor(Math.log2(e)),this._cubeSize=Math.pow(2,this._lodMax)}_dispose(){this._blurMaterial!==null&&this._blurMaterial.dispose(),this._ggxMaterial!==null&&this._ggxMaterial.dispose(),this._pingPongRenderTarget!==null&&this._pingPongRenderTarget.dispose();for(let e=0;e<this._lodMeshes.length;e++)this._lodMeshes[e].geometry.dispose()}_cleanup(e){this._renderer.setRenderTarget(Kf,Jf,Qf),this._renderer.xr.enabled=ed,e.scissorTest=!1,Hs(e,0,0,e.width,e.height)}_fromTexture(e,n){e.mapping===rr||e.mapping===Fr?this._setSize(e.image.length===0?16:e.image[0].width||e.image[0].image.width):this._setSize(e.image.width/4),Kf=this._renderer.getRenderTarget(),Jf=this._renderer.getActiveCubeFace(),Qf=this._renderer.getActiveMipmapLevel(),ed=this._renderer.xr.enabled,this._renderer.xr.enabled=!1;let r=n||this._allocateTargets();return this._textureToCubeUV(e,r),this._applyPMREM(r),this._cleanup(r),r}_allocateTargets(){let e=3*Math.max(this._cubeSize,112),n=4*this._cubeSize,r={magFilter:Vt,minFilter:Vt,generateMipmaps:!1,type:En,format:Nn,colorSpace:wo,depthBuffer:!1},s=Pg(e,n,r);if(this._pingPongRenderTarget===null||this._pingPongRenderTarget.width!==e||this._pingPongRenderTarget.height!==n){this._pingPongRenderTarget!==null&&this._dispose(),this._pingPongRenderTarget=Pg(e,n,r);let{_lodMax:o}=this;({lodMeshes:this._lodMeshes,sizeLods:this._sizeLods}=iE(o)),this._blurMaterial=sE(o,e,n),this._ggxMaterial=rE(o,e,n)}return s}_compileMaterial(e){let n=new kt(new Ft,e);this._renderer.compile(n,Jo)}_sceneToCubeUV(e,n,r,s,o){let c=new $t(90,1,n,r),u=[1,-1,1,1,1,1],h=[1,1,1,-1,-1,-1],d=this._renderer,f=d.autoClear,p=d.toneMapping;d.getClearColor(Rg),d.toneMapping=Zn,d.autoClear=!1,d.state.buffers.depth.getReversed()&&(d.setRenderTarget(s),d.clearDepth(),d.setRenderTarget(null)),this._backgroundBox===null&&(this._backgroundBox=new kt(new Ls,new Ir({name:"PMREM.Background",side:Gt,depthWrite:!1,depthTest:!1})));let v=this._backgroundBox,_=v.material,m=!1,w=e.background;w?w.isColor&&(_.color.copy(w),e.background=null,m=!0):(_.color.copy(Rg),m=!0);for(let T=0;T<6;T++){let y=T%3;y===0?(c.up.set(0,u[T],0),c.position.set(o.x,o.y,o.z),c.lookAt(o.x+h[T],o.y,o.z)):y===1?(c.up.set(0,0,u[T]),c.position.set(o.x,o.y,o.z),c.lookAt(o.x,o.y+h[T],o.z)):(c.up.set(0,u[T],0),c.position.set(o.x,o.y,o.z),c.lookAt(o.x,o.y,o.z+h[T]));let b=this._cubeSize;Hs(s,y*b,T>2?b:0,b,b),d.setRenderTarget(s),m&&d.render(v,c),d.render(e,c)}d.toneMapping=p,d.autoClear=f,e.background=w}_textureToCubeUV(e,n){let r=this._renderer,s=e.mapping===rr||e.mapping===Fr;s?(this._cubemapMaterial===null&&(this._cubemapMaterial=Lg()),this._cubemapMaterial.uniforms.flipEnvMap.value=e.isRenderTargetTexture===!1?-1:1):this._equirectMaterial===null&&(this._equirectMaterial=Ig());let o=s?this._cubemapMaterial:this._equirectMaterial,a=this._lodMeshes[0];a.material=o;let l=o.uniforms;l.envMap.value=e;let c=this._cubeSize;Hs(n,0,0,3*c,2*c),r.setRenderTarget(n),r.render(a,Jo)}_applyPMREM(e){let n=this._renderer,r=n.autoClear;n.autoClear=!1;let s=this._lodMeshes.length;for(let o=1;o<s;o++)this._applyGGXFilter(e,o-1,o);n.autoClear=r}_applyGGXFilter(e,n,r){let s=this._renderer,o=this._pingPongRenderTarget,a=this._ggxMaterial,l=this._lodMeshes[r];l.material=a;let c=a.uniforms,u=r/(this._lodMeshes.length-1),h=n/(this._lodMeshes.length-1),d=Math.sqrt(u*u-h*h),f=u*1.25,p=d*f,{_lodMax:g}=this,v=this._sizeLods[r],_=3*v*(r>g-Ws?r-g+Ws:0),m=4*(this._cubeSize-v);c.envMap.value=e.texture,c.roughness.value=p,c.mipInt.value=g-n,Hs(o,_,m,3*v,2*v),s.setRenderTarget(o),s.render(l,Jo),c.envMap.value=o.texture,c.roughness.value=0,c.mipInt.value=g-r,Hs(e,_,m,3*v,2*v),s.setRenderTarget(e),s.render(l,Jo)}_blur(e,n,r,s){let o=this._pingPongRenderTarget,a=Math.min(s,Math.PI)/Math.SQRT2;this._blurPass(e,o,n,r,a),this._blurPass(o,e,r,r,a)}_blurPass(e,n,r,s,o){let a=this._renderer,l=this._blurMaterial,c=this._lodMeshes[s];c.material=l;let u=l.uniforms;u.envMap.value=e.texture,u.sigma.value=o,u.mipInt.value=this._lodMax-r;let h=this._sizeLods[s],d=3*h*(s>this._lodMax-Ws?s-this._lodMax+Ws:0),f=4*(this._cubeSize-h);Hs(n,d,f,3*h,2*h),a.setRenderTarget(n),a.render(c,Jo)}};function iE(i){let e=[],n=[],r=i,s=i-Ws+1+Q1;for(let o=0;o<s;o++){let a=Math.pow(2,r);e.push(a);let l=1/(a-2),c=-l,u=1+l,h=[c,c,u,c,u,u,c,c,u,u,c,u],d=6,f=6,p=3,g=new Float32Array(p*f*d),v=new Float32Array(p*f*d);for(let m=0;m<d;m++){let w=m%3*2/3-1,T=m>2?0:-1,y=[w,T,0,w+2/3,T,0,w+2/3,T+1,0,w,T,0,w+2/3,T+1,0,w,T+1,0];g.set(y,p*f*m);for(let b=0;b<f;b++){let S=h[b*2]*2-1,A=h[b*2+1]*2-1;m===0?Br.set(1,A,S):m===1?Br.set(-S,1,-A):m===2?Br.set(-S,A,1):m===3?Br.set(-1,A,-S):m===4?Br.set(-S,-1,A):Br.set(S,A,-1),Br.toArray(v,(m*f+b)*p)}}let _=new Ft;_.setAttribute("position",new pn(g,p)),_.setAttribute("outputDirection",new pn(v,p)),n.push(new kt(_,null)),r>Ws&&r--}return{lodMeshes:n,sizeLods:e}}function Pg(i,e,n){let r=new Zt(i,e,n);return r.texture.mapping=Wo,r.texture.name="PMREM.cubeUv",r.scissorTest=!0,r}function Hs(i,e,n,r,s){i.viewport.set(e,n,r,s),i.scissor.set(e,n,r,s)}function rE(i,e,n){return new Jt({name:"PMREMGGXConvolution",defines:{GGX_SAMPLES:tE,CUBEUV_TEXEL_WIDTH:1/e,CUBEUV_TEXEL_HEIGHT:1/n,CUBEUV_MAX_MIP:`${i}.0`},uniforms:{envMap:{value:null},roughness:{value:0},mipInt:{value:0}},vertexShader:ou(),fragmentShader:`

			precision highp float;
			precision highp int;

			varying vec3 vOutputDirection;

			uniform sampler2D envMap;
			uniform float roughness;
			uniform float mipInt;

			#define ENVMAP_TYPE_CUBE_UV
			#include <cube_uv_reflection_fragment>

			#define PI 3.14159265359

			// Van der Corput radical inverse
			float radicalInverse_VdC(uint bits) {
				bits = (bits << 16u) | (bits >> 16u);
				bits = ((bits & 0x55555555u) << 1u) | ((bits & 0xAAAAAAAAu) >> 1u);
				bits = ((bits & 0x33333333u) << 2u) | ((bits & 0xCCCCCCCCu) >> 2u);
				bits = ((bits & 0x0F0F0F0Fu) << 4u) | ((bits & 0xF0F0F0F0u) >> 4u);
				bits = ((bits & 0x00FF00FFu) << 8u) | ((bits & 0xFF00FF00u) >> 8u);
				return float(bits) * 2.3283064365386963e-10; // / 0x100000000
			}

			// Hammersley sequence
			vec2 hammersley(uint i, uint N) {
				return vec2(float(i) / float(N), radicalInverse_VdC(i));
			}

			// GGX VNDF importance sampling (Eric Heitz 2018)
			// "Sampling the GGX Distribution of Visible Normals"
			// https://jcgt.org/published/0007/04/01/
			vec3 importanceSampleGGX_VNDF(vec2 Xi, vec3 V, float roughness) {
				float alpha = roughness * roughness;

				// Section 4.1: Orthonormal basis
				vec3 T1 = vec3(1.0, 0.0, 0.0);
				vec3 T2 = cross(V, T1);

				// Section 4.2: Parameterization of projected area
				float r = sqrt(Xi.x);
				float phi = 2.0 * PI * Xi.y;
				float t1 = r * cos(phi);
				float t2 = r * sin(phi);
				float s = 0.5 * (1.0 + V.z);
				t2 = (1.0 - s) * sqrt(1.0 - t1 * t1) + s * t2;

				// Section 4.3: Reprojection onto hemisphere
				vec3 Nh = t1 * T1 + t2 * T2 + sqrt(max(0.0, 1.0 - t1 * t1 - t2 * t2)) * V;

				// Section 3.4: Transform back to ellipsoid configuration
				return normalize(vec3(alpha * Nh.x, alpha * Nh.y, max(0.0, Nh.z)));
			}

			void main() {
				vec3 N = normalize(vOutputDirection);
				vec3 V = N; // Assume view direction equals normal for pre-filtering

				vec3 prefilteredColor = vec3(0.0);
				float totalWeight = 0.0;

				// For very low roughness, just sample the environment directly
				if (roughness < 0.001) {
					gl_FragColor = vec4(bilinearCubeUV(envMap, N, mipInt), 1.0);
					return;
				}

				// Tangent space basis for VNDF sampling
				vec3 up = abs(N.z) < 0.999 ? vec3(0.0, 0.0, 1.0) : vec3(1.0, 0.0, 0.0);
				vec3 tangent = normalize(cross(up, N));
				vec3 bitangent = cross(N, tangent);

				for(uint i = 0u; i < uint(GGX_SAMPLES); i++) {
					vec2 Xi = hammersley(i, uint(GGX_SAMPLES));

					// For PMREM, V = N, so in tangent space V is always (0, 0, 1)
					vec3 H_tangent = importanceSampleGGX_VNDF(Xi, vec3(0.0, 0.0, 1.0), roughness);

					// Transform H back to world space
					vec3 H = normalize(tangent * H_tangent.x + bitangent * H_tangent.y + N * H_tangent.z);
					vec3 L = normalize(2.0 * dot(V, H) * H - V);

					float NdotL = max(dot(N, L), 0.0);

					if(NdotL > 0.0) {
						// Sample environment at fixed mip level
						// VNDF importance sampling handles the distribution filtering
						vec3 sampleColor = bilinearCubeUV(envMap, L, mipInt);

						// Weight by NdotL for the split-sum approximation
						// VNDF PDF naturally accounts for the visible microfacet distribution
						prefilteredColor += sampleColor * NdotL;
						totalWeight += NdotL;
					}
				}

				if (totalWeight > 0.0) {
					prefilteredColor = prefilteredColor / totalWeight;
				}

				gl_FragColor = vec4(prefilteredColor, 1.0);
			}
		`,blending:On,depthTest:!1,depthWrite:!1})}function sE(i,e,n){return new Jt({name:"SphericalGaussianBlur",defines:{SAMPLES:eE,CUBEUV_TEXEL_WIDTH:1/e,CUBEUV_TEXEL_HEIGHT:1/n,CUBEUV_MAX_MIP:`${i}.0`},uniforms:{envMap:{value:null},sigma:{value:0},mipInt:{value:0}},vertexShader:ou(),fragmentShader:`

			precision highp float;
			precision highp int;

			varying vec3 vOutputDirection;

			uniform sampler2D envMap;
			uniform float sigma;
			uniform float mipInt;

			#define ENVMAP_TYPE_CUBE_UV
			#include <cube_uv_reflection_fragment>

			#define PI 3.14159265359
			#define GOLDEN_ANGLE 2.39996322973

			void main() {

				if ( sigma == 0.0 ) {

					gl_FragColor = vec4( bilinearCubeUV( envMap, vOutputDirection, mipInt ), 1.0 );
					return;

				}

				vec3 outputDirection = normalize( vOutputDirection );

				vec3 up = abs( outputDirection.z ) < 0.999 ? vec3( 0.0, 0.0, 1.0 ) : vec3( 1.0, 0.0, 0.0 );
				vec3 tangent = normalize( cross( up, outputDirection ) );
				vec3 bitangent = cross( outputDirection, tangent );

				// Truncate the kernel at three standard deviations or at the antipode.
				float thetaMax = min( 3.0 * sigma, PI );
				float truncation = 1.0 - exp( - 0.5 * thetaMax * thetaMax / ( sigma * sigma ) );

				vec3 accumColor = vec3( 0.0 );
				float accumWeight = 0.0;

				for ( int i = 0; i < SAMPLES; i ++ ) {

					// Stratified inverse-CDF sampling of the Gaussian, placed on a golden-angle spiral.
					float stratum = ( float( i ) + 0.5 ) / float( SAMPLES );
					float theta = sigma * sqrt( - 2.0 * log( 1.0 - stratum * truncation ) );
					float phi = float( i ) * GOLDEN_ANGLE;

					vec3 offset = cos( phi ) * tangent + sin( phi ) * bitangent;
					vec3 sampleDirection = cos( theta ) * outputDirection + sin( theta ) * offset;

					// Correct the planar sample density to solid angle.
					float weight = sin( theta ) / theta;

					accumColor += weight * bilinearCubeUV( envMap, sampleDirection, mipInt );
					accumWeight += weight;

				}

				gl_FragColor = vec4( accumColor / accumWeight, 1.0 );

			}
		`,blending:On,depthTest:!1,depthWrite:!1})}function Ig(){return new Jt({name:"EquirectangularToCubeUV",uniforms:{envMap:{value:null}},vertexShader:ou(),fragmentShader:`

			precision mediump float;
			precision mediump int;

			varying vec3 vOutputDirection;

			uniform sampler2D envMap;

			#include <common>

			void main() {

				vec3 outputDirection = normalize( vOutputDirection );
				vec2 uv = equirectUv( outputDirection );

				gl_FragColor = vec4( texture2D ( envMap, uv ).rgb, 1.0 );

			}
		`,blending:On,depthTest:!1,depthWrite:!1})}function Lg(){return new Jt({name:"CubemapToCubeUV",uniforms:{envMap:{value:null},flipEnvMap:{value:-1}},vertexShader:ou(),fragmentShader:`

			precision mediump float;
			precision mediump int;

			uniform float flipEnvMap;

			varying vec3 vOutputDirection;

			uniform samplerCube envMap;

			void main() {

				gl_FragColor = textureCube( envMap, vec3( flipEnvMap * vOutputDirection.x, vOutputDirection.yz ) );

			}
		`,blending:On,depthTest:!1,depthWrite:!1})}function ou(){return`

		precision mediump float;
		precision mediump int;

		attribute vec3 outputDirection;

		varying vec3 vOutputDirection;

		void main() {

			vOutputDirection = outputDirection;
			gl_Position = vec4( position, 1.0 );

		}
	`}var ru=class extends Zt{constructor(e=1,n={}){super(e,e,n),this.isWebGLCubeRenderTarget=!0;let r={width:e,height:e,depth:1},s=[r,r,r,r,r,r];this.texture=new Po(s),this._setTextureOptions(n),this.texture.isRenderTargetTexture=!0}fromEquirectangularTexture(e,n){this.texture.type=n.type,this.texture.colorSpace=n.colorSpace,this.texture.generateMipmaps=n.generateMipmaps,this.texture.minFilter=n.minFilter,this.texture.magFilter=n.magFilter;let r={uniforms:{tEquirect:{value:null}},vertexShader:`

				varying vec3 vWorldDirection;

				vec3 transformDirection( in vec3 dir, in mat4 matrix ) {

					return normalize( ( matrix * vec4( dir, 0.0 ) ).xyz );

				}

				void main() {

					vWorldDirection = transformDirection( position, modelMatrix );

					#include <begin_vertex>
					#include <project_vertex>

				}
			`,fragmentShader:`

				uniform sampler2D tEquirect;

				varying vec3 vWorldDirection;

				#include <common>

				void main() {

					vec3 direction = normalize( vWorldDirection );

					vec2 sampleUV = equirectUv( direction );

					gl_FragColor = texture2D( tEquirect, sampleUV );

				}
			`},s=new Ls(5,5,5),o=new Jt({name:"CubemapFromEquirect",uniforms:kr(r.uniforms),vertexShader:r.vertexShader,fragmentShader:r.fragmentShader,side:Gt,blending:On});o.uniforms.tEquirect.value=n;let a=new kt(s,o),l=n.minFilter;return n.minFilter===sr&&(n.minFilter=Vt),new lc(1,10,this).update(e,a),n.minFilter=l,a.geometry.dispose(),a.material.dispose(),this}clear(e,n=!0,r=!0,s=!0){let o=e.getRenderTarget();for(let a=0;a<6;a++)e.setRenderTarget(this,a),e.clear(n,r,s);e.setRenderTarget(o)}};function oE(i){let e=new WeakMap,n=new WeakMap,r=null;function s(f,p=!1){return f==null?null:p?a(f):o(f)}function o(f){if(f&&f.isTexture){let p=f.mapping;if(p===fc||p===dc)if(e.has(f)){let g=e.get(f).texture;return l(g,f.mapping)}else{let g=f.image;if(g&&g.height>0){let v=new ru(g.height);return v.fromEquirectangularTexture(i,f),e.set(f,v),f.addEventListener("dispose",u),l(v.texture,f.mapping)}else return null}}return f}function a(f){if(f&&f.isTexture){let p=f.mapping,g=p===fc||p===dc,v=p===rr||p===Fr;if(g||v){let _=n.get(f),m=_!==void 0?_.texture.pmremVersion:0;if(f.isRenderTargetTexture&&f.pmremVersion!==m)return r===null&&(r=new iu(i)),_=g?r.fromEquirectangular(f,_):r.fromCubemap(f,_),_.texture.pmremVersion=f.pmremVersion,n.set(f,_),_.texture;if(_!==void 0)return _.texture;{let w=f.image;return g&&w&&w.height>0||v&&w&&c(w)?(r===null&&(r=new iu(i)),_=g?r.fromEquirectangular(f):r.fromCubemap(f),_.texture.pmremVersion=f.pmremVersion,n.set(f,_),f.addEventListener("dispose",h),_.texture):null}}}return f}function l(f,p){return p===fc?f.mapping=rr:p===dc&&(f.mapping=Fr),f}function c(f){let p=0,g=6;for(let v=0;v<g;v++)f[v]!==void 0&&p++;return p===g}function u(f){let p=f.target;p.removeEventListener("dispose",u);let g=e.get(p);g!==void 0&&(e.delete(p),g.dispose())}function h(f){let p=f.target;p.removeEventListener("dispose",h);let g=n.get(p);g!==void 0&&(n.delete(p),g.dispose())}function d(){e=new WeakMap,n=new WeakMap,r!==null&&(r.dispose(),r=null)}return{get:s,dispose:d}}function aE(i){let e={};function n(r){if(e[r]!==void 0)return e[r];let s=i.getExtension(r);return e[r]=s,s}return{has:function(r){return n(r)!==null},init:function(){n("EXT_color_buffer_float"),n("WEBGL_clip_cull_distance"),n("OES_texture_float_linear"),n("EXT_color_buffer_half_float"),n("WEBGL_multisampled_render_to_texture"),n("WEBGL_render_shared_exponent")},get:function(r){let s=n(r);return s===null&&Rr("WebGLRenderer: "+r+" extension not supported."),s}}}function lE(i,e,n,r){let s={},o=new WeakMap;function a(d){let f=d.target;f.index!==null&&e.remove(f.index);for(let g in f.attributes)e.remove(f.attributes[g]);f.removeEventListener("dispose",a),delete s[f.id];let p=o.get(f);p&&(e.remove(p),o.delete(f)),r.releaseStatesOfGeometry(f),f.isInstancedBufferGeometry===!0&&delete f._maxInstanceCount,n.memory.geometries--}function l(d,f){return s[f.id]===!0||(f.addEventListener("dispose",a),s[f.id]=!0,n.memory.geometries++),f}function c(d){let f=d.attributes;for(let p in f)e.update(f[p],i.ARRAY_BUFFER)}function u(d){let f=[],p=d.index,g=d.attributes.position,v=0;if(g===void 0)return;if(p!==null){let w=p.array;v=p.version;for(let T=0,y=w.length;T<y;T+=3){let b=w[T+0],S=w[T+1],A=w[T+2];f.push(b,S,S,A,A,b)}}else{let w=g.array;v=g.version;for(let T=0,y=w.length/3-1;T<y;T+=3){let b=T+0,S=T+1,A=T+2;f.push(b,S,S,A,A,b)}}let _=new(g.count>=65535?Co:To)(f,1);_.version=v;let m=o.get(d);m&&e.remove(m),o.set(d,_)}function h(d){let f=o.get(d);if(f){let p=d.index;p!==null&&f.version<p.version&&u(d)}else u(d);return o.get(d)}return{get:l,update:c,getWireframeAttribute:h}}function cE(i,e,n){let r;function s(d){r=d}let o,a;function l(d){o=d.type,a=d.bytesPerElement}function c(d,f){i.drawElements(r,f,o,d*a),n.update(f,r,1)}function u(d,f,p){p!==0&&(i.drawElementsInstanced(r,f,o,d*a,p),n.update(f,r,p))}function h(d,f,p){if(p===0)return;e.get("WEBGL_multi_draw").multiDrawElementsWEBGL(r,f,0,o,d,0,p);let v=0;for(let _=0;_<p;_++)v+=f[_];n.update(v,r,1)}this.setMode=s,this.setIndex=l,this.render=c,this.renderInstances=u,this.renderMultiDraw=h}function uE(i){let e={geometries:0,textures:0},n={frame:0,calls:0,triangles:0,points:0,lines:0};function r(o,a,l){switch(n.calls++,a){case i.TRIANGLES:n.triangles+=l*(o/3);break;case i.LINES:n.lines+=l*(o/2);break;case i.LINE_STRIP:n.lines+=l*(o-1);break;case i.LINE_LOOP:n.lines+=l*o;break;case i.POINTS:n.points+=l*o;break;default:ze("WebGLInfo: Unknown draw mode:",a);break}}function s(){n.calls=0,n.triangles=0,n.points=0,n.lines=0}return{memory:e,render:n,programs:null,autoReset:!0,reset:s,update:r}}function hE(i,e,n){let r=new WeakMap,s=new vt;function o(a,l,c){let u=a.morphTargetInfluences,h=l.morphAttributes.position||l.morphAttributes.normal||l.morphAttributes.color,d=h!==void 0?h.length:0,f=r.get(l);if(f===void 0||f.count!==d){let C=function(){A.dispose(),r.delete(l),l.removeEventListener("dispose",C)};f!==void 0&&f.texture.dispose();let p=l.morphAttributes.position!==void 0,g=l.morphAttributes.normal!==void 0,v=l.morphAttributes.color!==void 0,_=l.morphAttributes.position||[],m=l.morphAttributes.normal||[],w=l.morphAttributes.color||[],T=0;p===!0&&(T=1),g===!0&&(T=2),v===!0&&(T=3);let y=l.attributes.position.count*T,b=1;y>e.maxTextureSize&&(b=Math.ceil(y/e.maxTextureSize),y=e.maxTextureSize);let S=new Float32Array(y*b*4*d),A=new Eo(S,y,b,d);A.type=Jn,A.needsUpdate=!0;let x=T*4;for(let L=0;L<d;L++){let P=_[L],O=m[L],U=w[L],M=y*b*4*L;for(let I=0;I<P.count;I++){let D=I*x;p===!0&&(s.fromBufferAttribute(P,I),S[M+D+0]=s.x,S[M+D+1]=s.y,S[M+D+2]=s.z,S[M+D+3]=0),g===!0&&(s.fromBufferAttribute(O,I),S[M+D+4]=s.x,S[M+D+5]=s.y,S[M+D+6]=s.z,S[M+D+7]=0),v===!0&&(s.fromBufferAttribute(U,I),S[M+D+8]=s.x,S[M+D+9]=s.y,S[M+D+10]=s.z,S[M+D+11]=U.itemSize===4?s.w:1)}}f={count:d,texture:A,size:new fe(y,b)},r.set(l,f),l.addEventListener("dispose",C)}if(a.isInstancedMesh===!0&&a.morphTexture!==null)c.getUniforms().setValue(i,"morphTexture",a.morphTexture,n);else{let p=0;for(let v=0;v<u.length;v++)p+=u[v];let g=l.morphTargetsRelative?1:1-p;c.getUniforms().setValue(i,"morphTargetBaseInfluence",g),c.getUniforms().setValue(i,"morphTargetInfluences",u)}c.getUniforms().setValue(i,"morphTargetsTexture",f.texture,n),c.getUniforms().setValue(i,"morphTargetsTextureSize",f.size)}return{update:o}}function fE(i,e,n,r,s){let o=new WeakMap;function a(u){let h=s.render.frame,d=u.geometry,f=e.get(u,d);if(o.get(f)!==h&&(e.update(f),o.set(f,h)),u.isInstancedMesh&&(u.hasEventListener("dispose",c)===!1&&u.addEventListener("dispose",c),o.get(u)!==h&&(n.update(u.instanceMatrix,i.ARRAY_BUFFER),u.instanceColor!==null&&n.update(u.instanceColor,i.ARRAY_BUFFER),o.set(u,h))),u.isSkinnedMesh){let p=u.skeleton;o.get(p)!==h&&(p.update(),o.set(p,h))}return f}function l(){o=new WeakMap}function c(u){let h=u.target;h.removeEventListener("dispose",c),r.releaseStatesOfObject(h),n.remove(h.instanceMatrix),h.instanceColor!==null&&n.remove(h.instanceColor)}return{update:a,dispose:l}}var dE={[wf]:"LINEAR_TONE_MAPPING",[Mf]:"REINHARD_TONE_MAPPING",[Ef]:"CINEON_TONE_MAPPING",[Af]:"ACES_FILMIC_TONE_MAPPING",[Cf]:"AGX_TONE_MAPPING",[Rf]:"NEUTRAL_TONE_MAPPING",[Tf]:"CUSTOM_TONE_MAPPING"};function pE(i,e,n,r,s,o){let a=new Zt(e,n,{type:i,depthBuffer:s,stencilBuffer:o,samples:r?4:0,storeMultisampledDepthBuffer:!1,storeMultisampledStencilBuffer:!1,resolveDepthBuffer:!1,resolveStencilBuffer:!1}),l=null,c=null,u=new Ft;u.setAttribute("position",new _t([-1,3,0,-1,-1,0,3,-1,0],3)),u.setAttribute("uv",new _t([0,2,0,0,2,0],2));let h=new Yl({uniforms:{tDiffuse:{value:null}},vertexShader:`
			precision highp float;

			uniform mat4 modelViewMatrix;
			uniform mat4 projectionMatrix;

			attribute vec3 position;
			attribute vec2 uv;

			varying vec2 vUv;

			void main() {
				vUv = uv;
				gl_Position = projectionMatrix * modelViewMatrix * vec4( position, 1.0 );
			}`,fragmentShader:`
			precision highp float;

			uniform sampler2D tDiffuse;

			varying vec2 vUv;

			#include <tonemapping_pars_fragment>
			#include <colorspace_pars_fragment>

			void main() {
				gl_FragColor = texture2D( tDiffuse, vUv );

				#ifdef LINEAR_TONE_MAPPING
					gl_FragColor.rgb = LinearToneMapping( gl_FragColor.rgb );
				#elif defined( REINHARD_TONE_MAPPING )
					gl_FragColor.rgb = ReinhardToneMapping( gl_FragColor.rgb );
				#elif defined( CINEON_TONE_MAPPING )
					gl_FragColor.rgb = CineonToneMapping( gl_FragColor.rgb );
				#elif defined( ACES_FILMIC_TONE_MAPPING )
					gl_FragColor.rgb = ACESFilmicToneMapping( gl_FragColor.rgb );
				#elif defined( AGX_TONE_MAPPING )
					gl_FragColor.rgb = AgXToneMapping( gl_FragColor.rgb );
				#elif defined( NEUTRAL_TONE_MAPPING )
					gl_FragColor.rgb = NeutralToneMapping( gl_FragColor.rgb );
				#elif defined( CUSTOM_TONE_MAPPING )
					gl_FragColor.rgb = CustomToneMapping( gl_FragColor.rgb );
				#endif

				#ifdef SRGB_TRANSFER
					gl_FragColor = sRGBTransferOETF( gl_FragColor );
				#endif
			}`,depthTest:!1,depthWrite:!1}),d=new kt(u,h),f=new nr(-1,1,1,-1,0,1),p=null,g=null,v=!1,_,m=null,w=[],T=!1;this.setSize=function(y,b){a.setSize(y,b),l!==null&&l.setSize(y,b),c!==null&&c.setSize(y,b);for(let S=0;S<w.length;S++){let A=w[S];A.setSize&&A.setSize(y,b)}},this.setEffects=function(y){w=y,T=w.length>0&&w[0].isRenderPass===!0;let b=a.width,S=a.height;w.length>0&&l===null&&(l=new Zt(b,S,{type:En,depthBuffer:!1,stencilBuffer:!1}),c=new Zt(b,S,{type:En,depthBuffer:!1,stencilBuffer:!1}));for(let A=0;A<w.length;A++){let x=w[A];x.setSize&&x.setSize(b,S)}},this.begin=function(y,b){if(v||y.toneMapping===Zn&&w.length===0)return!1;if(m=b,b!==null){let S=b.width,A=b.height;(a.width!==S||a.height!==A)&&this.setSize(S,A)}return T===!1&&y.setRenderTarget(a),_=y.toneMapping,y.toneMapping=Zn,!0},this.hasRenderPass=function(){return T},this.end=function(y,b){y.toneMapping=_,v=!0;let S=a,A=l;for(let x=0;x<w.length;x++){let C=w[x];C.enabled!==!1&&(C.render(y,A,S,b),C.needsSwap!==!1&&(S=A,A=A===l?c:l))}if(p!==y.outputColorSpace||g!==y.toneMapping){p=y.outputColorSpace,g=y.toneMapping,h.defines={},Ke.getTransfer(p)===rt&&(h.defines.SRGB_TRANSFER="");let x=dE[g];x&&(h.defines[x]=""),h.needsUpdate=!0}h.uniforms.tDiffuse.value=S.texture,y.setRenderTarget(m),y.render(d,f),m=null,v=!1},this.isCompositing=function(){return v},this.dispose=function(){a.dispose(),l!==null&&l.dispose(),c!==null&&c.dispose(),u.dispose(),h.dispose()}}var Jg=new ln,id=new Ji(1,1),Qg=new Eo,e0=new Ul,t0=new Po,Dg=[],Og=[],Ng=new Float32Array(16),Ug=new Float32Array(9),Fg=new Float32Array(4);function qs(i,e,n){let r=i[0];if(r<=0||r>0)return i;let s=e*n,o=Dg[s];if(o===void 0&&(o=new Float32Array(s),Dg[s]=o),e!==0){r.toArray(o,0);for(let a=1,l=0;a!==e;++a)l+=n,i[a].toArray(o,l)}return o}function Rt(i,e){if(i.length!==e.length)return!1;for(let n=0,r=i.length;n<r;n++)if(i[n]!==e[n])return!1;return!0}function Pt(i,e){for(let n=0,r=e.length;n<r;n++)i[n]=e[n]}function au(i,e){let n=Og[e];n===void 0&&(n=new Int32Array(e),Og[e]=n);for(let r=0;r!==e;++r)n[r]=i.allocateTextureUnit();return n}function mE(i,e){let n=this.cache;n[0]!==e&&(i.uniform1f(this.addr,e),n[0]=e)}function gE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y)&&(i.uniform2f(this.addr,e.x,e.y),n[0]=e.x,n[1]=e.y);else{if(Rt(n,e))return;i.uniform2fv(this.addr,e),Pt(n,e)}}function _E(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z)&&(i.uniform3f(this.addr,e.x,e.y,e.z),n[0]=e.x,n[1]=e.y,n[2]=e.z);else if(e.r!==void 0)(n[0]!==e.r||n[1]!==e.g||n[2]!==e.b)&&(i.uniform3f(this.addr,e.r,e.g,e.b),n[0]=e.r,n[1]=e.g,n[2]=e.b);else{if(Rt(n,e))return;i.uniform3fv(this.addr,e),Pt(n,e)}}function vE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z||n[3]!==e.w)&&(i.uniform4f(this.addr,e.x,e.y,e.z,e.w),n[0]=e.x,n[1]=e.y,n[2]=e.z,n[3]=e.w);else{if(Rt(n,e))return;i.uniform4fv(this.addr,e),Pt(n,e)}}function yE(i,e){let n=this.cache,r=e.elements;if(r===void 0){if(Rt(n,e))return;i.uniformMatrix2fv(this.addr,!1,e),Pt(n,e)}else{if(Rt(n,r))return;Fg.set(r),i.uniformMatrix2fv(this.addr,!1,Fg),Pt(n,r)}}function xE(i,e){let n=this.cache,r=e.elements;if(r===void 0){if(Rt(n,e))return;i.uniformMatrix3fv(this.addr,!1,e),Pt(n,e)}else{if(Rt(n,r))return;Ug.set(r),i.uniformMatrix3fv(this.addr,!1,Ug),Pt(n,r)}}function bE(i,e){let n=this.cache,r=e.elements;if(r===void 0){if(Rt(n,e))return;i.uniformMatrix4fv(this.addr,!1,e),Pt(n,e)}else{if(Rt(n,r))return;Ng.set(r),i.uniformMatrix4fv(this.addr,!1,Ng),Pt(n,r)}}function SE(i,e){let n=this.cache;n[0]!==e&&(i.uniform1i(this.addr,e),n[0]=e)}function wE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y)&&(i.uniform2i(this.addr,e.x,e.y),n[0]=e.x,n[1]=e.y);else{if(Rt(n,e))return;i.uniform2iv(this.addr,e),Pt(n,e)}}function ME(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z)&&(i.uniform3i(this.addr,e.x,e.y,e.z),n[0]=e.x,n[1]=e.y,n[2]=e.z);else{if(Rt(n,e))return;i.uniform3iv(this.addr,e),Pt(n,e)}}function EE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z||n[3]!==e.w)&&(i.uniform4i(this.addr,e.x,e.y,e.z,e.w),n[0]=e.x,n[1]=e.y,n[2]=e.z,n[3]=e.w);else{if(Rt(n,e))return;i.uniform4iv(this.addr,e),Pt(n,e)}}function AE(i,e){let n=this.cache;n[0]!==e&&(i.uniform1ui(this.addr,e),n[0]=e)}function TE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y)&&(i.uniform2ui(this.addr,e.x,e.y),n[0]=e.x,n[1]=e.y);else{if(Rt(n,e))return;i.uniform2uiv(this.addr,e),Pt(n,e)}}function CE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z)&&(i.uniform3ui(this.addr,e.x,e.y,e.z),n[0]=e.x,n[1]=e.y,n[2]=e.z);else{if(Rt(n,e))return;i.uniform3uiv(this.addr,e),Pt(n,e)}}function RE(i,e){let n=this.cache;if(e.x!==void 0)(n[0]!==e.x||n[1]!==e.y||n[2]!==e.z||n[3]!==e.w)&&(i.uniform4ui(this.addr,e.x,e.y,e.z,e.w),n[0]=e.x,n[1]=e.y,n[2]=e.z,n[3]=e.w);else{if(Rt(n,e))return;i.uniform4uiv(this.addr,e),Pt(n,e)}}function PE(i,e,n){let r=this.cache,s=n.allocateTextureUnit();r[0]!==s&&(i.uniform1i(this.addr,s),r[0]=s);let o;this.type===i.SAMPLER_2D_SHADOW?(id.compareFunction=n.isReversedDepthBuffer()?Qc:Jc,o=id):o=Jg,n.setTexture2D(e||o,s)}function IE(i,e,n){let r=this.cache,s=n.allocateTextureUnit();r[0]!==s&&(i.uniform1i(this.addr,s),r[0]=s),n.setTexture3D(e||e0,s)}function LE(i,e,n){let r=this.cache,s=n.allocateTextureUnit();r[0]!==s&&(i.uniform1i(this.addr,s),r[0]=s),n.setTextureCube(e||t0,s)}function DE(i,e,n){let r=this.cache,s=n.allocateTextureUnit();r[0]!==s&&(i.uniform1i(this.addr,s),r[0]=s),n.setTexture2DArray(e||Qg,s)}function OE(i){switch(i){case 5126:return mE;case 35664:return gE;case 35665:return _E;case 35666:return vE;case 35674:return yE;case 35675:return xE;case 35676:return bE;case 5124:case 35670:return SE;case 35667:case 35671:return wE;case 35668:case 35672:return ME;case 35669:case 35673:return EE;case 5125:return AE;case 36294:return TE;case 36295:return CE;case 36296:return RE;case 35678:case 36198:case 36298:case 36306:case 35682:return PE;case 35679:case 36299:case 36307:return IE;case 35680:case 36300:case 36308:case 36293:return LE;case 36289:case 36303:case 36311:case 36292:return DE}}function NE(i,e){i.uniform1fv(this.addr,e)}function UE(i,e){let n=qs(e,this.size,2);i.uniform2fv(this.addr,n)}function FE(i,e){let n=qs(e,this.size,3);i.uniform3fv(this.addr,n)}function kE(i,e){let n=qs(e,this.size,4);i.uniform4fv(this.addr,n)}function BE(i,e){let n=qs(e,this.size,4);i.uniformMatrix2fv(this.addr,!1,n)}function zE(i,e){let n=qs(e,this.size,9);i.uniformMatrix3fv(this.addr,!1,n)}function VE(i,e){let n=qs(e,this.size,16);i.uniformMatrix4fv(this.addr,!1,n)}function GE(i,e){i.uniform1iv(this.addr,e)}function HE(i,e){i.uniform2iv(this.addr,e)}function WE(i,e){i.uniform3iv(this.addr,e)}function XE(i,e){i.uniform4iv(this.addr,e)}function qE(i,e){i.uniform1uiv(this.addr,e)}function jE(i,e){i.uniform2uiv(this.addr,e)}function YE(i,e){i.uniform3uiv(this.addr,e)}function $E(i,e){i.uniform4uiv(this.addr,e)}function ZE(i,e,n){let r=this.cache,s=e.length,o=au(n,s);Rt(r,o)||(i.uniform1iv(this.addr,o),Pt(r,o));let a;this.type===i.SAMPLER_2D_SHADOW?a=id:a=Jg;for(let l=0;l!==s;++l)n.setTexture2D(e[l]||a,o[l])}function KE(i,e,n){let r=this.cache,s=e.length,o=au(n,s);Rt(r,o)||(i.uniform1iv(this.addr,o),Pt(r,o));for(let a=0;a!==s;++a)n.setTexture3D(e[a]||e0,o[a])}function JE(i,e,n){let r=this.cache,s=e.length,o=au(n,s);Rt(r,o)||(i.uniform1iv(this.addr,o),Pt(r,o));for(let a=0;a!==s;++a)n.setTextureCube(e[a]||t0,o[a])}function QE(i,e,n){let r=this.cache,s=e.length,o=au(n,s);Rt(r,o)||(i.uniform1iv(this.addr,o),Pt(r,o));for(let a=0;a!==s;++a)n.setTexture2DArray(e[a]||Qg,o[a])}function eA(i){switch(i){case 5126:return NE;case 35664:return UE;case 35665:return FE;case 35666:return kE;case 35674:return BE;case 35675:return zE;case 35676:return VE;case 5124:case 35670:return GE;case 35667:case 35671:return HE;case 35668:case 35672:return WE;case 35669:case 35673:return XE;case 5125:return qE;case 36294:return jE;case 36295:return YE;case 36296:return $E;case 35678:case 36198:case 36298:case 36306:case 35682:return ZE;case 35679:case 36299:case 36307:return KE;case 35680:case 36300:case 36308:case 36293:return JE;case 36289:case 36303:case 36311:case 36292:return QE}}var rd=class{constructor(e,n,r){this.id=e,this.addr=r,this.cache=[],this.type=n.type,this.setValue=OE(n.type)}},sd=class{constructor(e,n,r){this.id=e,this.addr=r,this.cache=[],this.type=n.type,this.size=n.size,this.setValue=eA(n.type)}},od=class{constructor(e){this.id=e,this.seq=[],this.map={}}setValue(e,n,r){let s=this.seq;for(let o=0,a=s.length;o!==a;++o){let l=s[o];l.setValue(e,n[l.id],r)}}},td=/(\w+)(\])?(\[|\.)?/g;function kg(i,e){i.seq.push(e),i.map[e.id]=e}function tA(i,e,n){let r=i.name,s=r.length;for(td.lastIndex=0;;){let o=td.exec(r),a=td.lastIndex,l=o[1],c=o[2]==="]",u=o[3];if(c&&(l=l|0),u===void 0||u==="["&&a+2===s){kg(n,u===void 0?new rd(l,i,e):new sd(l,i,e));break}else{let d=n.map[l];d===void 0&&(d=new od(l),kg(n,d)),n=d}}}var Xs=class{constructor(e,n){this.seq=[],this.map={};let r=e.getProgramParameter(n,e.ACTIVE_UNIFORMS);for(let a=0;a<r;++a){let l=e.getActiveUniform(n,a),c=e.getUniformLocation(n,l.name);tA(l,c,this)}let s=[],o=[];for(let a of this.seq)a.type===e.SAMPLER_2D_SHADOW||a.type===e.SAMPLER_CUBE_SHADOW||a.type===e.SAMPLER_2D_ARRAY_SHADOW?s.push(a):o.push(a);s.length>0&&(this.seq=s.concat(o))}setValue(e,n,r,s){let o=this.map[n];o!==void 0&&o.setValue(e,r,s)}setOptional(e,n,r){let s=n[r];s!==void 0&&this.setValue(e,r,s)}static upload(e,n,r,s){for(let o=0,a=n.length;o!==a;++o){let l=n[o],c=r[l.id];c.needsUpdate!==!1&&l.setValue(e,c.value,s)}}static seqWithValue(e,n){let r=[];for(let s=0,o=e.length;s!==o;++s){let a=e[s];a.id in n&&r.push(a)}return r}};function Bg(i,e,n){let r=i.createShader(e);return i.shaderSource(r,n),i.compileShader(r),r}var nA=37297,iA=0;function rA(i,e){let n=i.split(`
`),r=[],s=Math.max(e-6,0),o=Math.min(e+6,n.length);for(let a=s;a<o;a++){let l=a+1;r.push(`${l===e?">":" "} ${l}: ${n[a]}`)}return r.join(`
`)}var zg=new He;function sA(i){Ke._getMatrix(zg,Ke.workingColorSpace,i);let e=`mat3( ${zg.elements.map(n=>n.toFixed(4))} )`;switch(Ke.getTransfer(i)){case Mo:return[e,"LinearTransferOETF"];case rt:return[e,"sRGBTransferOETF"];default:return Ue("WebGLProgram: Unsupported color space: ",i),[e,"LinearTransferOETF"]}}function Vg(i,e,n){let r=i.getShaderParameter(e,i.COMPILE_STATUS),o=(i.getShaderInfoLog(e)||"").trim();if(r&&o==="")return"";let a=/ERROR: 0:(\d+)/.exec(o);if(a){let l=parseInt(a[1]);return n.toUpperCase()+`

`+o+`

`+rA(i.getShaderSource(e),l)}else return o}function oA(i,e){let n=sA(e);return[`vec4 ${i}( vec4 value ) {`,`	return ${n[1]}( vec4( value.rgb * ${n[0]}, value.a ) );`,"}"].join(`
`)}var aA={[wf]:"Linear",[Mf]:"Reinhard",[Ef]:"Cineon",[Af]:"ACESFilmic",[Cf]:"AgX",[Rf]:"Neutral",[Tf]:"Custom"};function lA(i,e){let n=aA[e];return n===void 0?(Ue("WebGLProgram: Unsupported toneMapping:",e),"vec3 "+i+"( vec3 color ) { return LinearToneMapping( color ); }"):"vec3 "+i+"( vec3 color ) { return "+n+"ToneMapping( color ); }"}var nu=new F;function cA(){Ke.getLuminanceCoefficients(nu);let i=nu.x.toFixed(4),e=nu.y.toFixed(4),n=nu.z.toFixed(4);return["float luminance( const in vec3 rgb ) {",`	const vec3 weights = vec3( ${i}, ${e}, ${n} );`,"	return dot( weights, rgb );","}"].join(`
`)}function uA(i){return[i.extensionClipCullDistance?"#extension GL_ANGLE_clip_cull_distance : require":"",i.extensionMultiDraw?"#extension GL_ANGLE_multi_draw : require":""].filter(ea).join(`
`)}function hA(i){let e=[];for(let n in i){let r=i[n];r!==!1&&e.push("#define "+n+" "+r)}return e.join(`
`)}function fA(i,e){let n={},r=i.getProgramParameter(e,i.ACTIVE_ATTRIBUTES);for(let s=0;s<r;s++){let o=i.getActiveAttrib(e,s),a=o.name,l=1;o.type===i.FLOAT_MAT2&&(l=2),o.type===i.FLOAT_MAT3&&(l=3),o.type===i.FLOAT_MAT4&&(l=4),n[a]={type:o.type,location:i.getAttribLocation(e,a),locationSize:l}}return n}function ea(i){return i!==""}function Gg(i,e){let n=e.numSpotLightShadows+e.numSpotLightMaps-e.numSpotLightShadowsWithMaps;return i.replace(/NUM_SUN_LIGHTS/g,e.numSunLights).replace(/NUM_DIR_LIGHTS/g,e.numDirLights).replace(/NUM_SPOT_LIGHTS/g,e.numSpotLights).replace(/NUM_SPOT_LIGHT_MAPS/g,e.numSpotLightMaps).replace(/NUM_SPOT_LIGHT_COORDS/g,n).replace(/NUM_RECT_AREA_LIGHTS/g,e.numRectAreaLights).replace(/NUM_POINT_LIGHTS/g,e.numPointLights).replace(/NUM_HEMI_LIGHTS/g,e.numHemiLights).replace(/NUM_SUN_LIGHT_SHADOWS/g,e.numSunLightShadows).replace(/NUM_DIR_LIGHT_SHADOWS/g,e.numDirLightShadows).replace(/NUM_SPOT_LIGHT_SHADOWS_WITH_MAPS/g,e.numSpotLightShadowsWithMaps).replace(/NUM_SPOT_LIGHT_SHADOWS/g,e.numSpotLightShadows).replace(/NUM_POINT_LIGHT_SHADOWS/g,e.numPointLightShadows)}function Hg(i,e){return i.replace(/NUM_CLIPPING_PLANES/g,e.numClippingPlanes).replace(/UNION_CLIPPING_PLANES/g,e.numClippingPlanes-e.numClipIntersection)}var dA=/^[ \t]*#include +<([\w\d./]+)>/gm;function ad(i){return i.replace(dA,mA)}var pA=new Map;function mA(i,e){let n=Ye[e];if(n===void 0){let r=pA.get(e);if(r!==void 0)n=Ye[r],Ue('WebGLRenderer: Shader chunk "%s" has been deprecated. Use "%s" instead.',e,r);else throw new Error("THREE.WebGLProgram: Can not resolve #include <"+e+">")}return ad(n)}var gA=/#pragma unroll_loop_start\s+for\s*\(\s*int\s+i\s*=\s*(\d+)\s*;\s*i\s*<\s*(\d+)\s*;\s*i\s*\+\+\s*\)\s*{([\s\S]+?)}\s+#pragma unroll_loop_end/g;function Wg(i){return i.replace(gA,_A)}function _A(i,e,n,r){let s="";for(let o=parseInt(e);o<parseInt(n);o++)s+=r.replace(/\[\s*i\s*\]/g,"[ "+o+" ]").replace(/UNROLLED_LOOP_INDEX/g,o);return s}function Xg(i){let e=`precision ${i.precision} float;
	precision ${i.precision} int;
	precision ${i.precision} sampler2D;
	precision ${i.precision} samplerCube;
	precision ${i.precision} sampler3D;
	precision ${i.precision} sampler2DArray;
	precision ${i.precision} sampler2DShadow;
	precision ${i.precision} samplerCubeShadow;
	precision ${i.precision} sampler2DArrayShadow;
	precision ${i.precision} isampler2D;
	precision ${i.precision} isampler3D;
	precision ${i.precision} isamplerCube;
	precision ${i.precision} isampler2DArray;
	precision ${i.precision} usampler2D;
	precision ${i.precision} usampler3D;
	precision ${i.precision} usamplerCube;
	precision ${i.precision} usampler2DArray;
	`;return i.precision==="highp"?e+=`
#define HIGH_PRECISION`:i.precision==="mediump"?e+=`
#define MEDIUM_PRECISION`:i.precision==="lowp"&&(e+=`
#define LOW_PRECISION`),e}var vA={[Ho]:"SHADOWMAP_TYPE_PCF",[Fs]:"SHADOWMAP_TYPE_VSM"};function yA(i){return vA[i.shadowMapType]||"SHADOWMAP_TYPE_BASIC"}var xA={[rr]:"ENVMAP_TYPE_CUBE",[Fr]:"ENVMAP_TYPE_CUBE",[Wo]:"ENVMAP_TYPE_CUBE_UV"};function bA(i){return i.envMap===!1?"ENVMAP_TYPE_CUBE":xA[i.envMapMode]||"ENVMAP_TYPE_CUBE"}var SA={[Fr]:"ENVMAP_MODE_REFRACTION"};function wA(i){return i.envMap===!1?"ENVMAP_MODE_REFLECTION":SA[i.envMapMode]||"ENVMAP_MODE_REFLECTION"}var MA={[hc]:"ENVMAP_BLENDING_MULTIPLY",[cg]:"ENVMAP_BLENDING_MIX",[ug]:"ENVMAP_BLENDING_ADD"};function EA(i){return i.envMap===!1?"ENVMAP_BLENDING_NONE":MA[i.combine]||"ENVMAP_BLENDING_NONE"}function AA(i){let e=i.envMapCubeUVHeight;if(e===null)return null;let n=Math.log2(e)-2,r=1/e;return{texelWidth:1/(3*Math.max(Math.pow(2,n),112)),texelHeight:r,maxMip:n}}function TA(i,e,n,r){let s=i.getContext(),o=n.defines,a=n.vertexShader,l=n.fragmentShader,c=yA(n),u=bA(n),h=wA(n),d=EA(n),f=AA(n),p=uA(n),g=hA(o),v=s.createProgram(),_,m,w=n.glslVersion?"#version "+n.glslVersion+`
`:"";n.isRawShaderMaterial?(_=["#define SHADER_TYPE "+n.shaderType,"#define SHADER_NAME "+n.shaderName,g].filter(ea).join(`
`),_.length>0&&(_+=`
`),m=["#define SHADER_TYPE "+n.shaderType,"#define SHADER_NAME "+n.shaderName,g].filter(ea).join(`
`),m.length>0&&(m+=`
`)):(_=[Xg(n),"#define SHADER_TYPE "+n.shaderType,"#define SHADER_NAME "+n.shaderName,g,n.extensionClipCullDistance?"#define USE_CLIP_DISTANCE":"",n.batching?"#define USE_BATCHING":"",n.batchingColor?"#define USE_BATCHING_COLOR":"",n.instancing?"#define USE_INSTANCING":"",n.instancingColor?"#define USE_INSTANCING_COLOR":"",n.instancingMorph?"#define USE_INSTANCING_MORPH":"",n.useFog&&n.fog?"#define USE_FOG":"",n.useFog&&n.fogExp2?"#define FOG_EXP2":"",n.map?"#define USE_MAP":"",n.envMap?"#define USE_ENVMAP":"",n.envMap?"#define "+h:"",n.lightMap?"#define USE_LIGHTMAP":"",n.aoMap?"#define USE_AOMAP":"",n.bumpMap?"#define USE_BUMPMAP":"",n.normalMap?"#define USE_NORMALMAP":"",n.normalMapObjectSpace?"#define USE_NORMALMAP_OBJECTSPACE":"",n.normalMapTangentSpace?"#define USE_NORMALMAP_TANGENTSPACE":"",n.displacementMap?"#define USE_DISPLACEMENTMAP":"",n.emissiveMap?"#define USE_EMISSIVEMAP":"",n.anisotropy?"#define USE_ANISOTROPY":"",n.anisotropyMap?"#define USE_ANISOTROPYMAP":"",n.clearcoatMap?"#define USE_CLEARCOATMAP":"",n.clearcoatRoughnessMap?"#define USE_CLEARCOAT_ROUGHNESSMAP":"",n.clearcoatNormalMap?"#define USE_CLEARCOAT_NORMALMAP":"",n.iridescenceMap?"#define USE_IRIDESCENCEMAP":"",n.iridescenceThicknessMap?"#define USE_IRIDESCENCE_THICKNESSMAP":"",n.specularMap?"#define USE_SPECULARMAP":"",n.specularColorMap?"#define USE_SPECULAR_COLORMAP":"",n.specularIntensityMap?"#define USE_SPECULAR_INTENSITYMAP":"",n.roughnessMap?"#define USE_ROUGHNESSMAP":"",n.metalnessMap?"#define USE_METALNESSMAP":"",n.alphaMap?"#define USE_ALPHAMAP":"",n.alphaHash?"#define USE_ALPHAHASH":"",n.transmission?"#define USE_TRANSMISSION":"",n.transmissionMap?"#define USE_TRANSMISSIONMAP":"",n.thicknessMap?"#define USE_THICKNESSMAP":"",n.sheenColorMap?"#define USE_SHEEN_COLORMAP":"",n.sheenRoughnessMap?"#define USE_SHEEN_ROUGHNESSMAP":"",n.mapUv?"#define MAP_UV "+n.mapUv:"",n.alphaMapUv?"#define ALPHAMAP_UV "+n.alphaMapUv:"",n.lightMapUv?"#define LIGHTMAP_UV "+n.lightMapUv:"",n.aoMapUv?"#define AOMAP_UV "+n.aoMapUv:"",n.emissiveMapUv?"#define EMISSIVEMAP_UV "+n.emissiveMapUv:"",n.bumpMapUv?"#define BUMPMAP_UV "+n.bumpMapUv:"",n.normalMapUv?"#define NORMALMAP_UV "+n.normalMapUv:"",n.displacementMapUv?"#define DISPLACEMENTMAP_UV "+n.displacementMapUv:"",n.metalnessMapUv?"#define METALNESSMAP_UV "+n.metalnessMapUv:"",n.roughnessMapUv?"#define ROUGHNESSMAP_UV "+n.roughnessMapUv:"",n.anisotropyMapUv?"#define ANISOTROPYMAP_UV "+n.anisotropyMapUv:"",n.clearcoatMapUv?"#define CLEARCOATMAP_UV "+n.clearcoatMapUv:"",n.clearcoatNormalMapUv?"#define CLEARCOAT_NORMALMAP_UV "+n.clearcoatNormalMapUv:"",n.clearcoatRoughnessMapUv?"#define CLEARCOAT_ROUGHNESSMAP_UV "+n.clearcoatRoughnessMapUv:"",n.iridescenceMapUv?"#define IRIDESCENCEMAP_UV "+n.iridescenceMapUv:"",n.iridescenceThicknessMapUv?"#define IRIDESCENCE_THICKNESSMAP_UV "+n.iridescenceThicknessMapUv:"",n.sheenColorMapUv?"#define SHEEN_COLORMAP_UV "+n.sheenColorMapUv:"",n.sheenRoughnessMapUv?"#define SHEEN_ROUGHNESSMAP_UV "+n.sheenRoughnessMapUv:"",n.specularMapUv?"#define SPECULARMAP_UV "+n.specularMapUv:"",n.specularColorMapUv?"#define SPECULAR_COLORMAP_UV "+n.specularColorMapUv:"",n.specularIntensityMapUv?"#define SPECULAR_INTENSITYMAP_UV "+n.specularIntensityMapUv:"",n.transmissionMapUv?"#define TRANSMISSIONMAP_UV "+n.transmissionMapUv:"",n.thicknessMapUv?"#define THICKNESSMAP_UV "+n.thicknessMapUv:"",n.vertexTangents&&n.flatShading===!1?"#define USE_TANGENT":"",n.vertexNormals?"#define HAS_NORMAL":"",n.vertexColors?"#define USE_COLOR":"",n.vertexAlphas?"#define USE_COLOR_ALPHA":"",n.vertexUv1s?"#define USE_UV1":"",n.vertexUv2s?"#define USE_UV2":"",n.vertexUv3s?"#define USE_UV3":"",n.pointsUvs?"#define USE_POINTS_UV":"",n.flatShading?"#define FLAT_SHADED":"",n.skinning?"#define USE_SKINNING":"",n.morphTargets?"#define USE_MORPHTARGETS":"",n.morphNormals&&n.flatShading===!1?"#define USE_MORPHNORMALS":"",n.morphColors?"#define USE_MORPHCOLORS":"",n.morphTargetsCount>0?"#define MORPHTARGETS_TEXTURE_STRIDE "+n.morphTextureStride:"",n.morphTargetsCount>0?"#define MORPHTARGETS_COUNT "+n.morphTargetsCount:"",n.doubleSided?"#define DOUBLE_SIDED":"",n.flipSided?"#define FLIP_SIDED":"",n.shadowMapEnabled?"#define USE_SHADOWMAP":"",n.shadowMapEnabled?"#define "+c:"",n.sizeAttenuation?"#define USE_SIZEATTENUATION":"",n.numLightProbes>0?"#define USE_LIGHT_PROBES":"",n.logarithmicDepthBuffer?"#define USE_LOGARITHMIC_DEPTH_BUFFER":"",n.reversedDepthBuffer?"#define USE_REVERSED_DEPTH_BUFFER":"","uniform mat4 modelMatrix;","uniform mat4 modelViewMatrix;","uniform mat4 projectionMatrix;","uniform mat4 viewMatrix;","uniform mat3 normalMatrix;","uniform vec3 cameraPosition;","uniform bool isOrthographic;","#ifdef USE_INSTANCING","	attribute mat4 instanceMatrix;","#endif","#ifdef USE_INSTANCING_COLOR","	attribute vec3 instanceColor;","#endif","#ifdef USE_INSTANCING_MORPH","	uniform sampler2D morphTexture;","#endif","attribute vec3 position;","attribute vec3 normal;","attribute vec2 uv;","#ifdef USE_UV1","	attribute vec2 uv1;","#endif","#ifdef USE_UV2","	attribute vec2 uv2;","#endif","#ifdef USE_UV3","	attribute vec2 uv3;","#endif","#ifdef USE_TANGENT","	attribute vec4 tangent;","#endif","#if defined( USE_COLOR_ALPHA )","	attribute vec4 color;","#elif defined( USE_COLOR )","	attribute vec3 color;","#endif","#ifdef USE_SKINNING","	attribute vec4 skinIndex;","	attribute vec4 skinWeight;","#endif",`
`].filter(ea).join(`
`),m=[Xg(n),"#define SHADER_TYPE "+n.shaderType,"#define SHADER_NAME "+n.shaderName,g,n.useFog&&n.fog?"#define USE_FOG":"",n.useFog&&n.fogExp2?"#define FOG_EXP2":"",n.alphaToCoverage?"#define ALPHA_TO_COVERAGE":"",n.map?"#define USE_MAP":"",n.matcap?"#define USE_MATCAP":"",n.envMap?"#define USE_ENVMAP":"",n.envMap?"#define "+u:"",n.envMap?"#define "+h:"",n.envMap?"#define "+d:"",f?"#define CUBEUV_TEXEL_WIDTH "+f.texelWidth:"",f?"#define CUBEUV_TEXEL_HEIGHT "+f.texelHeight:"",f?"#define CUBEUV_MAX_MIP "+f.maxMip+".0":"",n.lightMap?"#define USE_LIGHTMAP":"",n.aoMap?"#define USE_AOMAP":"",n.bumpMap?"#define USE_BUMPMAP":"",n.normalMap?"#define USE_NORMALMAP":"",n.normalMapObjectSpace?"#define USE_NORMALMAP_OBJECTSPACE":"",n.normalMapTangentSpace?"#define USE_NORMALMAP_TANGENTSPACE":"",n.packedNormalMap?"#define USE_PACKED_NORMALMAP":"",n.emissiveMap?"#define USE_EMISSIVEMAP":"",n.anisotropy?"#define USE_ANISOTROPY":"",n.anisotropyMap?"#define USE_ANISOTROPYMAP":"",n.clearcoat?"#define USE_CLEARCOAT":"",n.clearcoatMap?"#define USE_CLEARCOATMAP":"",n.clearcoatRoughnessMap?"#define USE_CLEARCOAT_ROUGHNESSMAP":"",n.clearcoatNormalMap?"#define USE_CLEARCOAT_NORMALMAP":"",n.dispersion?"#define USE_DISPERSION":"",n.retroreflection?"#define USE_RETROREFLECTION":"",n.iridescence?"#define USE_IRIDESCENCE":"",n.iridescenceMap?"#define USE_IRIDESCENCEMAP":"",n.iridescenceThicknessMap?"#define USE_IRIDESCENCE_THICKNESSMAP":"",n.specularMap?"#define USE_SPECULARMAP":"",n.specularColorMap?"#define USE_SPECULAR_COLORMAP":"",n.specularIntensityMap?"#define USE_SPECULAR_INTENSITYMAP":"",n.roughnessMap?"#define USE_ROUGHNESSMAP":"",n.metalnessMap?"#define USE_METALNESSMAP":"",n.alphaMap?"#define USE_ALPHAMAP":"",n.alphaTest?"#define USE_ALPHATEST":"",n.alphaHash?"#define USE_ALPHAHASH":"",n.sheen?"#define USE_SHEEN":"",n.sheenColorMap?"#define USE_SHEEN_COLORMAP":"",n.sheenRoughnessMap?"#define USE_SHEEN_ROUGHNESSMAP":"",n.transmission?"#define USE_TRANSMISSION":"",n.transmissionMap?"#define USE_TRANSMISSIONMAP":"",n.thicknessMap?"#define USE_THICKNESSMAP":"",n.vertexTangents&&n.flatShading===!1?"#define USE_TANGENT":"",n.vertexColors||n.instancingColor?"#define USE_COLOR":"",n.vertexAlphas||n.batchingColor?"#define USE_COLOR_ALPHA":"",n.vertexUv1s?"#define USE_UV1":"",n.vertexUv2s?"#define USE_UV2":"",n.vertexUv3s?"#define USE_UV3":"",n.pointsUvs?"#define USE_POINTS_UV":"",n.gradientMap?"#define USE_GRADIENTMAP":"",n.flatShading?"#define FLAT_SHADED":"",n.doubleSided?"#define DOUBLE_SIDED":"",n.flipSided?"#define FLIP_SIDED":"",n.shadowMapEnabled?"#define USE_SHADOWMAP":"",n.shadowMapEnabled?"#define "+c:"",n.premultipliedAlpha?"#define PREMULTIPLIED_ALPHA":"",n.numLightProbes>0?"#define USE_LIGHT_PROBES":"",n.numLightProbeGrids>0?"#define USE_LIGHT_PROBES_GRID":"",n.decodeVideoTexture?"#define DECODE_VIDEO_TEXTURE":"",n.decodeVideoTextureEmissive?"#define DECODE_VIDEO_TEXTURE_EMISSIVE":"",n.logarithmicDepthBuffer?"#define USE_LOGARITHMIC_DEPTH_BUFFER":"",n.reversedDepthBuffer?"#define USE_REVERSED_DEPTH_BUFFER":"","uniform mat4 viewMatrix;","uniform vec3 cameraPosition;","uniform bool isOrthographic;",n.toneMapping!==Zn?"#define TONE_MAPPING":"",n.toneMapping!==Zn?Ye.tonemapping_pars_fragment:"",n.toneMapping!==Zn?lA("toneMapping",n.toneMapping):"",n.dithering?"#define DITHERING":"",n.opaque?"#define OPAQUE":"",Ye.colorspace_pars_fragment,oA("linearToOutputTexel",n.outputColorSpace),cA(),n.useDepthPacking?"#define DEPTH_PACKING "+n.depthPacking:"",`
`].filter(ea).join(`
`)),a=ad(a),a=Gg(a,n),a=Hg(a,n),l=ad(l),l=Gg(l,n),l=Hg(l,n),a=Wg(a),l=Wg(l),n.isRawShaderMaterial!==!0&&(w=`#version 300 es
`,_=[p,"#define attribute in","#define varying out","#define texture2D texture"].join(`
`)+`
`+_,m=["#define varying in",n.glslVersion===kf?"":"layout(location = 0) out highp vec4 pc_fragColor;",n.glslVersion===kf?"":"#define gl_FragColor pc_fragColor","#define gl_FragDepthEXT gl_FragDepth","#define texture2D texture","#define textureCube texture","#define texture2DProj textureProj","#define texture2DLodEXT textureLod","#define texture2DProjLodEXT textureProjLod","#define textureCubeLodEXT textureLod","#define texture2DGradEXT textureGrad","#define texture2DProjGradEXT textureProjGrad","#define textureCubeGradEXT textureGrad"].join(`
`)+`
`+m);let T=w+_+a,y=w+m+l,b=Bg(s,s.VERTEX_SHADER,T),S=Bg(s,s.FRAGMENT_SHADER,y);s.attachShader(v,b),s.attachShader(v,S),n.index0AttributeName!==void 0?s.bindAttribLocation(v,0,n.index0AttributeName):n.hasPositionAttribute===!0&&s.bindAttribLocation(v,0,"position"),s.linkProgram(v);function A(P){if(i.debug.checkShaderErrors){let O=s.getProgramInfoLog(v)||"",U=s.getShaderInfoLog(b)||"",M=s.getShaderInfoLog(S)||"",I=O.trim(),D=U.trim(),k=M.trim(),q=!0,W=!0;if(s.getProgramParameter(v,s.LINK_STATUS)===!1)if(q=!1,typeof i.debug.onShaderError=="function")i.debug.onShaderError(s,v,b,S);else{let j=Vg(s,b,"vertex"),B=Vg(s,S,"fragment");ze("WebGLProgram: Shader Error "+s.getError()+" - VALIDATE_STATUS "+s.getProgramParameter(v,s.VALIDATE_STATUS)+`

Material Name: `+P.name+`
Material Type: `+P.type+`

Program Info Log: `+I+`
`+j+`
`+B)}else I!==""?Ue("WebGLProgram: Program Info Log:",I):(D===""||k==="")&&(W=!1);W&&(P.diagnostics={runnable:q,programLog:I,vertexShader:{log:D,prefix:_},fragmentShader:{log:k,prefix:m}})}s.deleteShader(b),s.deleteShader(S),x=new Xs(s,v),C=fA(s,v)}let x;this.getUniforms=function(){return x===void 0&&A(this),x};let C;this.getAttributes=function(){return C===void 0&&A(this),C};let L=n.rendererExtensionParallelShaderCompile===!1;return this.isReady=function(){return L===!1&&(L=s.getProgramParameter(v,nA)),L},this.destroy=function(){r.releaseStatesOfProgram(this),s.deleteProgram(v),this.program=void 0},this.type=n.shaderType,this.name=n.shaderName,this.id=iA++,this.cacheKey=e,this.usedTimes=1,this.program=v,this.vertexShader=b,this.fragmentShader=S,this}var CA=0,ld=class{constructor(){this.shaderCache=new Map,this.materialCache=new Map}update(e,n,r){let s=this._getShaderCacheForMaterial(e);return s.has(n)===!1&&(s.add(n),n.usedTimes++),s.has(r)===!1&&(s.add(r),r.usedTimes++),this}remove(e){let n=this.materialCache.get(e);for(let r of n)r.usedTimes--,r.usedTimes===0&&this.shaderCache.delete(r.code);return this.materialCache.delete(e),this}getVertexShaderStage(e){return this._getShaderStage(e.vertexShader)}getFragmentShaderStage(e){return this._getShaderStage(e.fragmentShader)}dispose(){this.shaderCache.clear(),this.materialCache.clear()}_getShaderCacheForMaterial(e){let n=this.materialCache,r=n.get(e);return r===void 0&&(r=new Set,n.set(e,r)),r}_getShaderStage(e){let n=this.shaderCache,r=n.get(e);return r===void 0&&(r=new cd(e),n.set(e,r)),r}},cd=class{constructor(e){this.id=CA++,this.code=e,this.usedTimes=0}};function RA(i){return i===ar||i===Zo||i===Ko}function PA(i,e,n,r,s,o){let a=new Cs,l=new ld,c=new Set,u=[],h=new Map,d=r.logarithmicDepthBuffer,f=r.precision,p={MeshDepthMaterial:"depth",MeshDistanceMaterial:"distance",MeshNormalMaterial:"normal",MeshBasicMaterial:"basic",MeshLambertMaterial:"lambert",MeshPhongMaterial:"phong",MeshToonMaterial:"toon",MeshStandardMaterial:"physical",MeshPhysicalMaterial:"physical",MeshMatcapMaterial:"matcap",LineBasicMaterial:"basic",LineDashedMaterial:"dashed",PointsMaterial:"points",ShadowMaterial:"shadow",SpriteMaterial:"sprite"};function g(x){return c.add(x),x===0?"uv":`uv${x}`}function v(x,C,L,P,O,U){let M=P.fog,I=O.geometry,D=x.isMeshStandardMaterial||x.isMeshLambertMaterial||x.isMeshPhongMaterial?P.environment:null,k=x.isMeshStandardMaterial||x.isMeshLambertMaterial&&!x.envMap||x.isMeshPhongMaterial&&!x.envMap,q=e.get(x.envMap||D,k),W=q&&q.mapping===Wo?q.image.height:null,j=p[x.type];x.precision!==null&&(f=r.getMaxPrecision(x.precision),f!==x.precision&&Ue("WebGLProgram.getParameters:",x.precision,"not supported, using",f,"instead."));let B=I.morphAttributes.position||I.morphAttributes.normal||I.morphAttributes.color,Q=B!==void 0?B.length:0,re=0;I.morphAttributes.position!==void 0&&(re=1),I.morphAttributes.normal!==void 0&&(re=2),I.morphAttributes.color!==void 0&&(re=3);let be,ee,oe,V;if(j){let ft=mi[j];be=ft.vertexShader,ee=ft.fragmentShader}else{be=x.vertexShader,ee=x.fragmentShader;let ft=l.getVertexShaderStage(x),nt=l.getFragmentShaderStage(x);l.update(x,ft,nt),oe=ft.id,V=nt.id}let Z=i.getRenderTarget(),ce=i.state.buffers.depth.getReversed(),Ee=O.isInstancedMesh===!0,ue=O.isBatchedMesh===!0,Be=!!x.map,ut=!!x.matcap,Ve=!!q,$e=!!x.aoMap,Je=!!x.lightMap,Ge=!!x.bumpMap&&x.wireframe===!1,tt=!!x.normalMap,Dt=!!x.displacementMap,fn=!!x.emissiveMap,St=!!x.metalnessMap,At=!!x.roughnessMap,H=x.anisotropy>0,Xt=x.clearcoat>0,at=x.dispersion>0,N=x.retroreflectivity>0,E=x.iridescence>0,X=x.sheen>0,K=x.transmission>0,te=H&&!!x.anisotropyMap,he=Xt&&!!x.clearcoatMap,de=Xt&&!!x.clearcoatNormalMap,ne=Xt&&!!x.clearcoatRoughnessMap,se=E&&!!x.iridescenceMap,pe=E&&!!x.iridescenceThicknessMap,De=X&&!!x.sheenColorMap,ve=X&&!!x.sheenRoughnessMap,me=!!x.specularMap,Oe=!!x.specularColorMap,Fe=!!x.specularIntensityMap,Xe=K&&!!x.transmissionMap,G=K&&!!x.thicknessMap,ge=!!x.gradientMap,ie=!!x.alphaMap,_e=x.alphaTest>0,Me=!!x.alphaHash,ae=!!x.extensions,Ne=Zn;x.toneMapped&&(Z===null||Z.isXRRenderTarget===!0)&&(Ne=i.toneMapping);let Ie={shaderID:j,shaderType:x.type,shaderName:x.name,vertexShader:be,fragmentShader:ee,defines:x.defines,customVertexShaderID:oe,customFragmentShaderID:V,isRawShaderMaterial:x.isRawShaderMaterial===!0,glslVersion:x.glslVersion,precision:f,batching:ue,batchingColor:ue&&O._colorsTexture!==null,instancing:Ee,instancingColor:Ee&&O.instanceColor!==null,instancingMorph:Ee&&O.morphTexture!==null,outputColorSpace:Z===null?i.outputColorSpace:Z.isXRRenderTarget===!0?Z.texture.colorSpace:Ke.workingColorSpace,alphaToCoverage:!!x.alphaToCoverage,map:Be,matcap:ut,envMap:Ve,envMapMode:Ve&&q.mapping,envMapCubeUVHeight:W,aoMap:$e,lightMap:Je,bumpMap:Ge,normalMap:tt,displacementMap:Dt,emissiveMap:fn,normalMapObjectSpace:tt&&x.normalMapType===dg,normalMapTangentSpace:tt&&x.normalMapType===Kc,packedNormalMap:tt&&x.normalMapType===Kc&&RA(x.normalMap.format),metalnessMap:St,roughnessMap:At,anisotropy:H,anisotropyMap:te,clearcoat:Xt,clearcoatMap:he,clearcoatNormalMap:de,clearcoatRoughnessMap:ne,dispersion:at,retroreflection:N,iridescence:E,iridescenceMap:se,iridescenceThicknessMap:pe,sheen:X,sheenColorMap:De,sheenRoughnessMap:ve,specularMap:me,specularColorMap:Oe,specularIntensityMap:Fe,transmission:K,transmissionMap:Xe,thicknessMap:G,gradientMap:ge,opaque:x.transparent===!1&&x.blending===ks&&x.alphaToCoverage===!1,alphaMap:ie,alphaTest:_e,alphaHash:Me,combine:x.combine,mapUv:Be&&g(x.map.channel),aoMapUv:$e&&g(x.aoMap.channel),lightMapUv:Je&&g(x.lightMap.channel),bumpMapUv:Ge&&g(x.bumpMap.channel),normalMapUv:tt&&g(x.normalMap.channel),displacementMapUv:Dt&&g(x.displacementMap.channel),emissiveMapUv:fn&&g(x.emissiveMap.channel),metalnessMapUv:St&&g(x.metalnessMap.channel),roughnessMapUv:At&&g(x.roughnessMap.channel),anisotropyMapUv:te&&g(x.anisotropyMap.channel),clearcoatMapUv:he&&g(x.clearcoatMap.channel),clearcoatNormalMapUv:de&&g(x.clearcoatNormalMap.channel),clearcoatRoughnessMapUv:ne&&g(x.clearcoatRoughnessMap.channel),iridescenceMapUv:se&&g(x.iridescenceMap.channel),iridescenceThicknessMapUv:pe&&g(x.iridescenceThicknessMap.channel),sheenColorMapUv:De&&g(x.sheenColorMap.channel),sheenRoughnessMapUv:ve&&g(x.sheenRoughnessMap.channel),specularMapUv:me&&g(x.specularMap.channel),specularColorMapUv:Oe&&g(x.specularColorMap.channel),specularIntensityMapUv:Fe&&g(x.specularIntensityMap.channel),transmissionMapUv:Xe&&g(x.transmissionMap.channel),thicknessMapUv:G&&g(x.thicknessMap.channel),alphaMapUv:ie&&g(x.alphaMap.channel),vertexTangents:!!I.attributes.tangent&&(tt||H),vertexNormals:!!I.attributes.normal,vertexColors:x.vertexColors,vertexAlphas:x.vertexColors===!0&&!!I.attributes.color&&I.attributes.color.itemSize===4,pointsUvs:O.isPoints===!0&&!!I.attributes.uv&&(Be||ie),fog:!!M,useFog:x.fog===!0,fogExp2:!!M&&M.isFogExp2,flatShading:x.wireframe===!1&&(x.flatShading===!0||I.attributes.normal===void 0&&tt===!1&&(x.isMeshLambertMaterial||x.isMeshPhongMaterial||x.isMeshStandardMaterial||x.isMeshPhysicalMaterial)),sizeAttenuation:x.sizeAttenuation===!0,logarithmicDepthBuffer:d,reversedDepthBuffer:ce,skinning:O.isSkinnedMesh===!0,hasPositionAttribute:I.attributes.position!==void 0,morphTargets:I.morphAttributes.position!==void 0,morphNormals:I.morphAttributes.normal!==void 0,morphColors:I.morphAttributes.color!==void 0,morphTargetsCount:Q,morphTextureStride:re,numSunLights:C.sun.length,numDirLights:C.directional.length,numPointLights:C.point.length,numSpotLights:C.spot.length,numSpotLightMaps:C.spotLightMap.length,numRectAreaLights:C.rectArea.length,numHemiLights:C.hemi.length,numSunLightShadows:C.sunShadowMap.length,numDirLightShadows:C.directionalShadowMap.length,numPointLightShadows:C.pointShadowMap.length,numSpotLightShadows:C.spotShadowMap.length,numSpotLightShadowsWithMaps:C.numSpotLightShadowsWithMaps,numLightProbes:C.numLightProbes,numLightProbeGrids:U.length,numClippingPlanes:o.numPlanes,numClipIntersection:o.numIntersection,dithering:x.dithering,shadowMapEnabled:i.shadowMap.enabled&&L.length>0,shadowMapType:i.shadowMap.type,toneMapping:Ne,decodeVideoTexture:Be&&x.map.isVideoTexture===!0&&Ke.getTransfer(x.map.colorSpace)===rt,decodeVideoTextureEmissive:fn&&x.emissiveMap.isVideoTexture===!0&&Ke.getTransfer(x.emissiveMap.colorSpace)===rt,premultipliedAlpha:x.premultipliedAlpha,doubleSided:x.side===di,flipSided:x.side===Gt,useDepthPacking:x.depthPacking>=0,depthPacking:x.depthPacking||0,index0AttributeName:x.index0AttributeName,extensionClipCullDistance:ae&&x.extensions.clipCullDistance===!0&&n.has("WEBGL_clip_cull_distance"),extensionMultiDraw:(ae&&x.extensions.multiDraw===!0||ue)&&n.has("WEBGL_multi_draw"),rendererExtensionParallelShaderCompile:n.has("KHR_parallel_shader_compile"),customProgramCacheKey:x.customProgramCacheKey()};return Ie.vertexUv1s=c.has(1),Ie.vertexUv2s=c.has(2),Ie.vertexUv3s=c.has(3),c.clear(),Ie}function _(x){let C=[];if(x.shaderID?C.push(x.shaderID):(C.push(x.customVertexShaderID),C.push(x.customFragmentShaderID)),x.defines!==void 0)for(let L in x.defines)C.push(L),C.push(x.defines[L]);return x.isRawShaderMaterial===!1&&(m(C,x),w(C,x),C.push(i.outputColorSpace)),C.push(x.customProgramCacheKey),C.join()}function m(x,C){x.push(C.precision),x.push(C.outputColorSpace),x.push(C.envMapMode),x.push(C.envMapCubeUVHeight),x.push(C.mapUv),x.push(C.alphaMapUv),x.push(C.lightMapUv),x.push(C.aoMapUv),x.push(C.bumpMapUv),x.push(C.normalMapUv),x.push(C.displacementMapUv),x.push(C.emissiveMapUv),x.push(C.metalnessMapUv),x.push(C.roughnessMapUv),x.push(C.anisotropyMapUv),x.push(C.clearcoatMapUv),x.push(C.clearcoatNormalMapUv),x.push(C.clearcoatRoughnessMapUv),x.push(C.iridescenceMapUv),x.push(C.iridescenceThicknessMapUv),x.push(C.sheenColorMapUv),x.push(C.sheenRoughnessMapUv),x.push(C.specularMapUv),x.push(C.specularColorMapUv),x.push(C.specularIntensityMapUv),x.push(C.transmissionMapUv),x.push(C.thicknessMapUv),x.push(C.combine),x.push(C.fogExp2),x.push(C.sizeAttenuation),x.push(C.morphTargetsCount),x.push(C.morphAttributeCount),x.push(C.numSunLights),x.push(C.numDirLights),x.push(C.numPointLights),x.push(C.numSpotLights),x.push(C.numSpotLightMaps),x.push(C.numHemiLights),x.push(C.numRectAreaLights),x.push(C.numSunLightShadows),x.push(C.numDirLightShadows),x.push(C.numPointLightShadows),x.push(C.numSpotLightShadows),x.push(C.numSpotLightShadowsWithMaps),x.push(C.numLightProbes),x.push(C.shadowMapType),x.push(C.toneMapping),x.push(C.numClippingPlanes),x.push(C.numClipIntersection),x.push(C.depthPacking)}function w(x,C){a.disableAll(),C.instancing&&a.enable(0),C.instancingColor&&a.enable(1),C.instancingMorph&&a.enable(2),C.matcap&&a.enable(3),C.envMap&&a.enable(4),C.normalMapObjectSpace&&a.enable(5),C.normalMapTangentSpace&&a.enable(6),C.clearcoat&&a.enable(7),C.iridescence&&a.enable(8),C.alphaTest&&a.enable(9),C.vertexColors&&a.enable(10),C.vertexAlphas&&a.enable(11),C.vertexUv1s&&a.enable(12),C.vertexUv2s&&a.enable(13),C.vertexUv3s&&a.enable(14),C.vertexTangents&&a.enable(15),C.anisotropy&&a.enable(16),C.alphaHash&&a.enable(17),C.batching&&a.enable(18),C.dispersion&&a.enable(19),C.retroreflection&&a.enable(24),C.batchingColor&&a.enable(20),C.gradientMap&&a.enable(21),C.packedNormalMap&&a.enable(22),C.vertexNormals&&a.enable(23),x.push(a.mask),a.disableAll(),C.fog&&a.enable(0),C.useFog&&a.enable(1),C.flatShading&&a.enable(2),C.logarithmicDepthBuffer&&a.enable(3),C.reversedDepthBuffer&&a.enable(4),C.skinning&&a.enable(5),C.morphTargets&&a.enable(6),C.morphNormals&&a.enable(7),C.morphColors&&a.enable(8),C.premultipliedAlpha&&a.enable(9),C.shadowMapEnabled&&a.enable(10),C.doubleSided&&a.enable(11),C.flipSided&&a.enable(12),C.useDepthPacking&&a.enable(13),C.dithering&&a.enable(14),C.transmission&&a.enable(15),C.sheen&&a.enable(16),C.opaque&&a.enable(17),C.pointsUvs&&a.enable(18),C.decodeVideoTexture&&a.enable(19),C.decodeVideoTextureEmissive&&a.enable(20),C.alphaToCoverage&&a.enable(21),C.numLightProbeGrids>0&&a.enable(22),C.hasPositionAttribute&&a.enable(23),x.push(a.mask)}function T(x){let C=p[x.type],L;if(C){let P=mi[C];L=eu.clone(P.uniforms)}else L=x.uniforms;return L}function y(x,C){let L=h.get(C);return L!==void 0?++L.usedTimes:(L=new TA(i,C,x,s),u.push(L),h.set(C,L)),L}function b(x){if(--x.usedTimes===0){let C=u.indexOf(x);u[C]=u[u.length-1],u.pop(),h.delete(x.cacheKey),x.destroy()}}function S(x){l.remove(x)}function A(){l.dispose()}return{getParameters:v,getProgramCacheKey:_,getUniforms:T,acquireProgram:y,releaseProgram:b,releaseShaderCache:S,programs:u,dispose:A}}function IA(){let i=new WeakMap;function e(a){return i.has(a)}function n(a){let l=i.get(a);return l===void 0&&(l={},i.set(a,l)),l}function r(a){i.delete(a)}function s(a,l,c){i.get(a)[l]=c}function o(){i=new WeakMap}return{has:e,get:n,remove:r,update:s,dispose:o}}function LA(i,e){return i.groupOrder!==e.groupOrder?i.groupOrder-e.groupOrder:i.renderOrder!==e.renderOrder?i.renderOrder-e.renderOrder:i.material.id!==e.material.id?i.material.id-e.material.id:i.materialVariant!==e.materialVariant?i.materialVariant-e.materialVariant:i.z!==e.z?i.z-e.z:i.id-e.id}function qg(i,e){return i.groupOrder!==e.groupOrder?i.groupOrder-e.groupOrder:i.renderOrder!==e.renderOrder?i.renderOrder-e.renderOrder:i.z!==e.z?e.z-i.z:i.id-e.id}function jg(){let i=[],e=0,n=[],r=[],s=[];function o(){e=0,n.length=0,r.length=0,s.length=0}function a(f){let p=0;return f.isInstancedMesh&&(p+=2),f.isSkinnedMesh&&(p+=1),p}function l(f,p,g,v,_,m){let w=i[e];return w===void 0?(w={id:f.id,object:f,geometry:p,material:g,materialVariant:a(f),groupOrder:v,renderOrder:f.renderOrder,z:_,group:m},i[e]=w):(w.id=f.id,w.object=f,w.geometry=p,w.material=g,w.materialVariant=a(f),w.groupOrder=v,w.renderOrder=f.renderOrder,w.z=_,w.group=m),e++,w}function c(f,p,g,v,_,m,w){w.reversedDepth===!0&&(_=-_);let T=l(f,p,g,v,_,m);g.transmission>0?r.push(T):g.transparent===!0?s.push(T):n.push(T)}function u(f,p,g,v,_,m){let w=l(f,p,g,v,_,m);g.transmission>0?r.unshift(w):g.transparent===!0?s.unshift(w):n.unshift(w)}function h(f,p){n.length>1&&n.sort(f||LA),r.length>1&&r.sort(p||qg),s.length>1&&s.sort(p||qg)}function d(){for(let f=e,p=i.length;f<p;f++){let g=i[f];if(g.id===null)break;g.id=null,g.object=null,g.geometry=null,g.material=null,g.group=null}}return{opaque:n,transmissive:r,transparent:s,init:o,push:c,unshift:u,finish:d,sort:h}}function DA(){let i=new WeakMap;function e(r,s){let o=i.get(r),a;return o===void 0?(a=new jg,i.set(r,[a])):s>=o.length?(a=new jg,o.push(a)):a=o[s],a}function n(){i=new WeakMap}return{get:e,dispose:n}}function OA(){let i={};return{get:function(e){if(i[e.id]!==void 0)return i[e.id];let n;switch(e.type){case"SunLight":case"DirectionalLight":n={direction:new F,color:new We};break;case"SpotLight":n={position:new F,direction:new F,color:new We,distance:0,coneCos:0,penumbraCos:0,decay:0};break;case"PointLight":n={position:new F,color:new We,distance:0,decay:0};break;case"HemisphereLight":n={direction:new F,skyColor:new We,groundColor:new We};break;case"RectAreaLight":n={color:new We,position:new F,halfWidth:new F,halfHeight:new F};break}return i[e.id]=n,n}}}function NA(){let i={};return{get:function(e){if(i[e.id]!==void 0)return i[e.id];let n;switch(e.type){case"SunLight":case"DirectionalLight":n={shadowIntensity:1,shadowBias:0,shadowNormalBias:0,shadowRadius:1,shadowMapSize:new fe};break;case"SpotLight":n={shadowIntensity:1,shadowBias:0,shadowNormalBias:0,shadowRadius:1,shadowMapSize:new fe};break;case"PointLight":n={shadowIntensity:1,shadowBias:0,shadowNormalBias:0,shadowRadius:1,shadowMapSize:new fe,shadowCameraNear:1,shadowCameraFar:1e3};break}return i[e.id]=n,n}}}var UA=0;function FA(i,e){return(e.castShadow?2:0)-(i.castShadow?2:0)+(e.map?1:0)-(i.map?1:0)}function kA(i){let e=new OA,n=NA(),r={version:0,hash:{sunLength:-1,directionalLength:-1,pointLength:-1,spotLength:-1,rectAreaLength:-1,hemiLength:-1,numSunShadows:-1,numDirectionalShadows:-1,numPointShadows:-1,numSpotShadows:-1,numSpotMaps:-1,numLightProbes:-1},ambient:[0,0,0],probe:[],sun:[],sunShadow:[],sunShadowMap:[],sunShadowMatrix:[],sunShadowCascade:[],directional:[],directionalShadow:[],directionalShadowMap:[],directionalShadowMatrix:[],spot:[],spotLightMap:[],spotShadow:[],spotShadowMap:[],spotLightMatrix:[],rectArea:[],rectAreaLTC1:null,rectAreaLTC2:null,point:[],pointShadow:[],pointShadowMap:[],pointShadowMatrix:[],hemi:[],numSpotLightShadowsWithMaps:0,numLightProbes:0};for(let u=0;u<9;u++)r.probe.push(new F);let s=new F,o=new st,a=new st;function l(u){let h=0,d=0,f=0;for(let O=0;O<9;O++)r.probe[O].set(0,0,0);let p=0,g=0,v=0,_=0,m=0,w=0,T=0,y=0,b=0,S=0,A=0,x=0,C=0,L=0;u.sort(FA);for(let O=0,U=u.length;O<U;O++){let M=u[O],I=M.color,D=M.intensity,k=M.distance,q=null;if(M.shadow&&M.shadow.map&&(M.shadow.map.texture.format===ar?q=M.shadow.map.texture:q=M.shadow.map.depthTexture||M.shadow.map.texture),M.isAmbientLight)h+=I.r*D,d+=I.g*D,f+=I.b*D;else if(M.isLightProbe){for(let W=0;W<9;W++)r.probe[W].addScaledVector(M.sh.coefficients[W],D);L++}else if(M.isSunLight){let W=e.get(M);if(W.color.copy(M.color).multiplyScalar(M.intensity),M.castShadow){let j=M.shadow,B=n.get(M);B.shadowIntensity=j.intensity,B.shadowBias=j.bias,B.shadowNormalBias=j.normalBias,B.shadowRadius=j.radius,B.shadowMapSize.copy(j.mapSize).multiply(j.getFrameExtents()),r.sunShadow[g]=B,r.sunShadowMap[g]=q;let Q=j.getViewportCount();for(let re=0;re<Q;re++)r.sunShadowMatrix[v+re]=j.getMatrix(re),r.sunShadowCascade[v+re]=j._cascadeData[re];v+=Q,g++}r.sun[p]=W,p++}else if(M.isDirectionalLight){let W=e.get(M);if(W.color.copy(M.color).multiplyScalar(M.intensity),M.castShadow){let j=M.shadow,B=n.get(M);B.shadowIntensity=j.intensity,B.shadowBias=j.bias,B.shadowNormalBias=j.normalBias,B.shadowRadius=j.radius,B.shadowMapSize=j.mapSize,r.directionalShadow[_]=B,r.directionalShadowMap[_]=q,r.directionalShadowMatrix[_]=M.shadow.matrix,b++}r.directional[_]=W,_++}else if(M.isSpotLight){let W=e.get(M);W.position.setFromMatrixPosition(M.matrixWorld),W.color.copy(I).multiplyScalar(D),W.distance=k,W.coneCos=Math.cos(M.angle),W.penumbraCos=Math.cos(M.angle*(1-M.penumbra)),W.decay=M.decay,r.spot[w]=W;let j=M.shadow;if(M.map&&(r.spotLightMap[x]=M.map,x++,j.updateMatrices(M),M.castShadow&&C++),r.spotLightMatrix[w]=j.matrix,M.castShadow){let B=n.get(M);B.shadowIntensity=j.intensity,B.shadowBias=j.bias,B.shadowNormalBias=j.normalBias,B.shadowRadius=j.radius,B.shadowMapSize=j.mapSize,r.spotShadow[w]=B,r.spotShadowMap[w]=q,A++}w++}else if(M.isRectAreaLight){let W=e.get(M);W.color.copy(I).multiplyScalar(D),W.halfWidth.set(M.width*.5,0,0),W.halfHeight.set(0,M.height*.5,0),r.rectArea[T]=W,T++}else if(M.isPointLight){let W=e.get(M);if(W.color.copy(M.color).multiplyScalar(M.intensity),W.distance=M.distance,W.decay=M.decay,M.castShadow){let j=M.shadow,B=n.get(M);B.shadowIntensity=j.intensity,B.shadowBias=j.bias,B.shadowNormalBias=j.normalBias,B.shadowRadius=j.radius,B.shadowMapSize=j.mapSize,B.shadowCameraNear=j.camera.near,B.shadowCameraFar=j.camera.far,r.pointShadow[m]=B,r.pointShadowMap[m]=q,r.pointShadowMatrix[m]=M.shadow.matrix,S++}r.point[m]=W,m++}else if(M.isHemisphereLight){let W=e.get(M);W.skyColor.copy(M.color).multiplyScalar(D),W.groundColor.copy(M.groundColor).multiplyScalar(D),r.hemi[y]=W,y++}}T>0&&(i.has("OES_texture_float_linear")===!0?(r.rectAreaLTC1=ye.LTC_FLOAT_1,r.rectAreaLTC2=ye.LTC_FLOAT_2):(r.rectAreaLTC1=ye.LTC_HALF_1,r.rectAreaLTC2=ye.LTC_HALF_2)),r.ambient[0]=h,r.ambient[1]=d,r.ambient[2]=f;let P=r.hash;(P.sunLength!==p||P.directionalLength!==_||P.pointLength!==m||P.spotLength!==w||P.rectAreaLength!==T||P.hemiLength!==y||P.numSunShadows!==g||P.numDirectionalShadows!==b||P.numPointShadows!==S||P.numSpotShadows!==A||P.numSpotMaps!==x||P.numLightProbes!==L)&&(r.sun.length=p,r.directional.length=_,r.spot.length=w,r.rectArea.length=T,r.point.length=m,r.hemi.length=y,r.sunShadow.length=g,r.sunShadowMap.length=g,r.sunShadowMatrix.length=v,r.sunShadowCascade.length=v,r.directionalShadow.length=b,r.directionalShadowMap.length=b,r.directionalShadowMatrix.length=b,r.pointShadow.length=S,r.pointShadowMap.length=S,r.pointShadowMatrix.length=S,r.spotShadow.length=A,r.spotShadowMap.length=A,r.spotLightMatrix.length=A+x-C,r.spotLightMap.length=x,r.numSpotLightShadowsWithMaps=C,r.numLightProbes=L,P.sunLength=p,P.directionalLength=_,P.pointLength=m,P.spotLength=w,P.rectAreaLength=T,P.hemiLength=y,P.numSunShadows=g,P.numDirectionalShadows=b,P.numPointShadows=S,P.numSpotShadows=A,P.numSpotMaps=x,P.numLightProbes=L,r.version=UA++)}function c(u,h){let d=0,f=0,p=0,g=0,v=0,_=0,m=h.matrixWorldInverse;for(let w=0,T=u.length;w<T;w++){let y=u[w];if(y.isSunLight){let b=r.sun[d];b.direction.setFromMatrixPosition(y.matrixWorld),b.direction.transformDirection(m),d++}else if(y.isDirectionalLight){let b=r.directional[f];b.direction.setFromMatrixPosition(y.matrixWorld),s.setFromMatrixPosition(y.target.matrixWorld),b.direction.sub(s),b.direction.transformDirection(m),f++}else if(y.isSpotLight){let b=r.spot[g];b.position.setFromMatrixPosition(y.matrixWorld),b.position.applyMatrix4(m),b.direction.setFromMatrixPosition(y.matrixWorld),s.setFromMatrixPosition(y.target.matrixWorld),b.direction.sub(s),b.direction.transformDirection(m),g++}else if(y.isRectAreaLight){let b=r.rectArea[v];b.position.setFromMatrixPosition(y.matrixWorld),b.position.applyMatrix4(m),a.identity(),o.copy(y.matrixWorld),o.premultiply(m),a.extractRotation(o),b.halfWidth.set(y.width*.5,0,0),b.halfHeight.set(0,y.height*.5,0),b.halfWidth.applyMatrix4(a),b.halfHeight.applyMatrix4(a),v++}else if(y.isPointLight){let b=r.point[p];b.position.setFromMatrixPosition(y.matrixWorld),b.position.applyMatrix4(m),p++}else if(y.isHemisphereLight){let b=r.hemi[_];b.direction.setFromMatrixPosition(y.matrixWorld),b.direction.transformDirection(m),_++}}}return{setup:l,setupView:c,state:r}}function Yg(i){let e=new kA(i),n=[],r=[],s=[];function o(f){d.camera=f,n.length=0,r.length=0,s.length=0}function a(f){n.push(f)}function l(f){r.push(f)}function c(f){s.push(f)}function u(){e.setup(n)}function h(f){e.setupView(n,f)}let d={lightsArray:n,shadowsArray:r,lightProbeGridArray:s,camera:null,lights:e,transmissionRenderTarget:{},textureUnits:0};return{init:o,state:d,setupLights:u,setupLightsView:h,pushLight:a,pushShadow:l,pushLightProbeGrid:c}}function BA(i){let e=new WeakMap;function n(s,o=0){let a=e.get(s),l;return a===void 0?(l=new Yg(i),e.set(s,[l])):o>=a.length?(l=new Yg(i),a.push(l)):l=a[o],l}function r(){e=new WeakMap}return{get:n,dispose:r}}var zA=`void main() {
	gl_Position = vec4( position, 1.0 );
}`,VA=`uniform sampler2D shadow_pass;
uniform vec2 resolution;
uniform float radius;
void main() {
	const float samples = float( VSM_SAMPLES );
	float mean = 0.0;
	float squared_mean = 0.0;
	float uvStride = samples <= 1.0 ? 0.0 : 2.0 / ( samples - 1.0 );
	float uvStart = samples <= 1.0 ? 0.0 : - 1.0;
	for ( float i = 0.0; i < samples; i ++ ) {
		float uvOffset = uvStart + i * uvStride;
		#ifdef HORIZONTAL_PASS
			vec2 distribution = texture2D( shadow_pass, ( gl_FragCoord.xy + vec2( uvOffset, 0.0 ) * radius ) / resolution ).rg;
			mean += distribution.x;
			squared_mean += distribution.y * distribution.y + distribution.x * distribution.x;
		#else
			float depth = texture2D( shadow_pass, ( gl_FragCoord.xy + vec2( 0.0, uvOffset ) * radius ) / resolution ).r;
			mean += depth;
			squared_mean += depth * depth;
		#endif
	}
	mean = mean / samples;
	squared_mean = squared_mean / samples;
	float std_dev = sqrt( max( 0.0, squared_mean - mean * mean ) );
	gl_FragColor = vec4( mean, std_dev, 0.0, 1.0 );
}`,GA=[new F(1,0,0),new F(-1,0,0),new F(0,1,0),new F(0,-1,0),new F(0,0,1),new F(0,0,-1)],HA=[new F(0,-1,0),new F(0,-1,0),new F(0,0,1),new F(0,0,-1),new F(0,-1,0),new F(0,-1,0)],$g=new st,Qo=new F,nd=new F;function WA(i,e,n){let r=new Ps,s=new fe,o=new fe,a=new vt,l=new $l,c=new Zl,u={},h=n.maxTextureSize,d={[ir]:Gt,[Gt]:ir,[di]:di},f=new Jt({defines:{VSM_SAMPLES:8},uniforms:{shadow_pass:{value:null},resolution:{value:new fe},radius:{value:4}},vertexShader:zA,fragmentShader:VA}),p=f.clone();p.defines.HORIZONTAL_PASS=1;let g=new Ft;g.setAttribute("position",new pn(new Float32Array([-1,-1,.5,3,-1,.5,-1,3,.5]),3));let v=new kt(g,f),_=this;this.enabled=!1,this.autoUpdate=!0,this.needsUpdate=!1,this.type=Ho;let m=this.type;this.render=function(S,A,x){if(_.enabled===!1||_.autoUpdate===!1&&_.needsUpdate===!1||S.length===0)return;this.type===Wm&&(Ue("WebGLShadowMap: PCFSoftShadowMap has been removed. Using PCFShadowMap instead."),this.type=Ho);let C=i.getRenderTarget(),L=i.getActiveCubeFace(),P=i.getActiveMipmapLevel(),O=i.state;O.setBlending(On),O.buffers.depth.getReversed()===!0?O.buffers.color.setClear(0,0,0,0):O.buffers.color.setClear(1,1,1,1),O.buffers.depth.setTest(!0),O.setScissorTest(!1);let U=m!==this.type;U&&A.traverse(function(M){M.material&&(Array.isArray(M.material)?M.material.forEach(I=>I.needsUpdate=!0):M.material.needsUpdate=!0)});for(let M=0,I=S.length;M<I;M++){let D=S[M],k=D.shadow;if(k===void 0){Ue("WebGLShadowMap:",D,"has no shadow.");continue}if(k.autoUpdate===!1&&k.needsUpdate===!1)continue;s.copy(k.mapSize);let q=k.getFrameExtents();s.multiply(q),o.copy(k.mapSize),(s.x>h||s.y>h)&&(s.x>h&&(o.x=Math.floor(h/q.x),s.x=o.x*q.x,k.mapSize.x=o.x),s.y>h&&(o.y=Math.floor(h/q.y),s.y=o.y*q.y,k.mapSize.y=o.y));let W=i.state.buffers.depth.getReversed();if(k.camera._reversedDepth=W,k.map===null||U===!0){if(k.map!==null&&(k.map.depthTexture!==null&&(k.map.depthTexture.dispose(),k.map.depthTexture=null),k.map.dispose()),this.type===Fs){if(D.isPointLight){Ue("WebGLShadowMap: VSM shadow maps are not supported for PointLights. Use PCF or BasicShadowMap instead.");continue}k.map=new Zt(s.x,s.y,{format:ar,type:En,minFilter:Vt,magFilter:Vt,generateMipmaps:!1}),k.map.texture.name=D.name+".shadowMap",k.map.depthTexture=new Ji(s.x,s.y,Jn),k.map.depthTexture.name=D.name+".shadowMapDepth",k.map.depthTexture.format=fi,k.map.depthTexture.compareFunction=null,k.map.depthTexture.minFilter=Nt,k.map.depthTexture.magFilter=Nt}else D.isPointLight?(k.map=new ru(s.x),k.map.depthTexture=new zl(s.x,Kn)):(k.map=new Zt(s.x,s.y),k.map.depthTexture=new Ji(s.x,s.y,Kn)),k.map.depthTexture.name=D.name+".shadowMap",k.map.depthTexture.format=fi,this.type===Ho?(k.map.depthTexture.compareFunction=W?Qc:Jc,k.map.depthTexture.minFilter=Vt,k.map.depthTexture.magFilter=Vt):(k.map.depthTexture.compareFunction=null,k.map.depthTexture.minFilter=Nt,k.map.depthTexture.magFilter=Nt);k.camera.updateProjectionMatrix()}k.map.isWebGLCubeRenderTarget!==!0&&(k.map.width!==s.x||k.map.height!==s.y)&&k.map.setSize(s.x,s.y);let j=k.map.isWebGLCubeRenderTarget?6:k.getViewportCount();D.isPointLight!==!0&&k.updateMatrices(D,x);for(let B=0;B<j;B++){let Q=k.getCamera(B);if(D.isPointLight){let re=k.camera,be=k.matrix,ee=D.distance||re.far;ee!==re.far&&(re.far=ee,re.updateProjectionMatrix()),Qo.setFromMatrixPosition(D.matrixWorld),re.position.copy(Qo),nd.copy(re.position),nd.add(GA[B]),re.up.copy(HA[B]),re.lookAt(nd),re.updateMatrixWorld(),be.makeTranslation(-Qo.x,-Qo.y,-Qo.z),$g.multiplyMatrices(re.projectionMatrix,re.matrixWorldInverse),k._frustum.setFromProjectionMatrix($g,re.coordinateSystem,re.reversedDepth)}if(k.map.isWebGLCubeRenderTarget)i.setRenderTarget(k.map,B),i.clear();else{B===0&&(i.setRenderTarget(k.map),i.clear());let re=k.getViewport(B);a.set(o.x*re.x,o.y*re.y,o.x*re.z,o.y*re.w),O.viewport(a)}r=k.getFrustum(B),y(A,x,Q,D,this.type)}k.isPointLightShadow!==!0&&this.type===Fs&&w(k,x),k.needsUpdate=!1}m=this.type,_.needsUpdate=!1,i.setRenderTarget(C,L,P)};function w(S,A){let x=e.update(v);f.defines.VSM_SAMPLES!==S.blurSamples&&(f.defines.VSM_SAMPLES=S.blurSamples,p.defines.VSM_SAMPLES=S.blurSamples,f.needsUpdate=!0,p.needsUpdate=!0),S.mapPass===null?S.mapPass=new Zt(s.x,s.y,{format:ar,type:En}):(S.mapPass.width!==S.map.width||S.mapPass.height!==S.map.height)&&S.mapPass.setSize(S.map.width,S.map.height),f.uniforms.shadow_pass.value=S.map.depthTexture,f.uniforms.resolution.value.set(S.map.width,S.map.height),f.uniforms.radius.value=S.radius,i.setRenderTarget(S.mapPass),i.clear(),i.renderBufferDirect(A,null,x,f,v,null),p.uniforms.shadow_pass.value=S.mapPass.texture,p.uniforms.resolution.value.set(S.map.width,S.map.height),p.uniforms.radius.value=S.radius,i.setRenderTarget(S.map),i.clear(),i.renderBufferDirect(A,null,x,p,v,null)}function T(S,A,x,C){let L=null,P=x.isPointLight===!0?S.customDistanceMaterial:S.customDepthMaterial;if(P!==void 0)L=P;else if(L=x.isPointLight===!0?c:l,i.localClippingEnabled&&A.clipShadows===!0&&Array.isArray(A.clippingPlanes)&&A.clippingPlanes.length!==0||A.displacementMap&&A.displacementScale!==0||A.alphaMap&&A.alphaTest>0||A.map&&A.alphaTest>0||A.alphaToCoverage===!0){let O=L.uuid,U=A.uuid,M=u[O];M===void 0&&(M={},u[O]=M);let I=M[U];I===void 0&&(I=L.clone(),M[U]=I,A.addEventListener("dispose",b)),L=I}if(L.visible=A.visible,L.wireframe=A.wireframe,C===Fs?L.side=A.shadowSide!==null?A.shadowSide:A.side:L.side=A.shadowSide!==null?A.shadowSide:d[A.side],L.alphaMap=A.alphaMap,L.alphaTest=A.alphaToCoverage===!0?.5:A.alphaTest,L.map=A.map,L.clipShadows=A.clipShadows,L.clippingPlanes=A.clippingPlanes,L.clipIntersection=A.clipIntersection,L.displacementMap=A.displacementMap,L.displacementScale=A.displacementScale,L.displacementBias=A.displacementBias,L.wireframeLinewidth=A.wireframeLinewidth,L.linewidth=A.linewidth,x.isPointLight===!0&&L.isMeshDistanceMaterial===!0){let O=i.properties.get(L);O.light=x}return L}function y(S,A,x,C,L){if(S.visible===!1)return;if(S.layers.test(A.layers)&&(S.isMesh||S.isLine||S.isPoints)&&(S.castShadow||S.receiveShadow&&L===Fs)&&(!S.frustumCulled||S.intersectsFrustum(r))){S.modelViewMatrix.multiplyMatrices(x.matrixWorldInverse,S.matrixWorld);let U=e.update(S),M=S.material;if(Array.isArray(M)){let I=U.groups;for(let D=0,k=I.length;D<k;D++){let q=I[D],W=M[q.materialIndex];if(W&&W.visible){let j=T(S,W,C,L);S.onBeforeShadow(i,S,A,x,U,j,q),i.renderBufferDirect(x,null,U,j,S,q),S.onAfterShadow(i,S,A,x,U,j,q)}}}else if(M.visible){let I=T(S,M,C,L);S.onBeforeShadow(i,S,A,x,U,I,null),i.renderBufferDirect(x,null,U,I,S,null),S.onAfterShadow(i,S,A,x,U,I,null)}}let O=S.children;for(let U=0,M=O.length;U<M;U++)y(O[U],A,x,C,L)}function b(S){S.target.removeEventListener("dispose",b);for(let x in u){let C=u[x],L=S.target.uuid;L in C&&(C[L].dispose(),delete C[L])}}}function XA(i,e){function n(){let G=!1,ge=new vt,ie=null,_e=new vt(0,0,0,0);return{setMask:function(Me){ie!==Me&&!G&&(i.colorMask(Me,Me,Me,Me),ie=Me)},setLocked:function(Me){G=Me},setClear:function(Me,ae,Ne,Ie,ft){ft===!0&&(Me*=Ie,ae*=Ie,Ne*=Ie),ge.set(Me,ae,Ne,Ie),_e.equals(ge)===!1&&(i.clearColor(Me,ae,Ne,Ie),_e.copy(ge))},reset:function(){G=!1,ie=null,_e.set(-1,0,0,0)}}}function r(){let G=!1,ge=!1,ie=null,_e=null,Me=null;return{setReversed:function(ae){if(ge!==ae){let Ne=e.get("EXT_clip_control");ae?Ne.clipControlEXT(Ne.LOWER_LEFT_EXT,Ne.ZERO_TO_ONE_EXT):Ne.clipControlEXT(Ne.LOWER_LEFT_EXT,Ne.NEGATIVE_ONE_TO_ONE_EXT),ge=ae;let Ie=Me;Me=null,this.setClear(Ie)}},getReversed:function(){return ge},setTest:function(ae){ae?Z(i.DEPTH_TEST):ce(i.DEPTH_TEST)},setMask:function(ae){ie!==ae&&!G&&(i.depthMask(ae),ie=ae)},setFunc:function(ae){if(ge&&(ae=Eg[ae]),_e!==ae){switch(ae){case Ml:i.depthFunc(i.NEVER);break;case El:i.depthFunc(i.ALWAYS);break;case Al:i.depthFunc(i.LESS);break;case Ss:i.depthFunc(i.LEQUAL);break;case Tl:i.depthFunc(i.EQUAL);break;case Cl:i.depthFunc(i.GEQUAL);break;case Rl:i.depthFunc(i.GREATER);break;case Pl:i.depthFunc(i.NOTEQUAL);break;default:i.depthFunc(i.LEQUAL)}_e=ae}},setLocked:function(ae){G=ae},setClear:function(ae){Me!==ae&&(Me=ae,ge&&(ae=1-ae),i.clearDepth(ae))},reset:function(){G=!1,ie=null,_e=null,Me=null,ge=!1}}}function s(){let G=!1,ge=null,ie=null,_e=null,Me=null,ae=null,Ne=null,Ie=null,ft=null;return{setTest:function(nt){G||(nt?Z(i.STENCIL_TEST):ce(i.STENCIL_TEST))},setMask:function(nt){ge!==nt&&!G&&(i.stencilMask(nt),ge=nt)},setFunc:function(nt,Hn,ai){(ie!==nt||_e!==Hn||Me!==ai)&&(i.stencilFunc(nt,Hn,ai),ie=nt,_e=Hn,Me=ai)},setOp:function(nt,Hn,ai){(ae!==nt||Ne!==Hn||Ie!==ai)&&(i.stencilOp(nt,Hn,ai),ae=nt,Ne=Hn,Ie=ai)},setLocked:function(nt){G=nt},setClear:function(nt){ft!==nt&&(i.clearStencil(nt),ft=nt)},reset:function(){G=!1,ge=null,ie=null,_e=null,Me=null,ae=null,Ne=null,Ie=null,ft=null}}}let o=new n,a=new r,l=new s,c=new WeakMap,u=new WeakMap,h={},d={},f={},p=new WeakMap,g=[],v=null,_=!1,m=null,w=null,T=null,y=null,b=null,S=null,A=null,x=new We(0,0,0),C=0,L=!1,P=null,O=null,U=null,M=null,I=null,D=i.getParameter(i.MAX_COMBINED_TEXTURE_IMAGE_UNITS),k=!1,q=0,W=i.getParameter(i.VERSION);W.indexOf("WebGL")!==-1?(q=parseFloat(/^WebGL (\d)/.exec(W)[1]),k=q>=1):W.indexOf("OpenGL ES")!==-1&&(q=parseFloat(/^OpenGL ES (\d)/.exec(W)[1]),k=q>=2);let j=null,B={},Q=i.getParameter(i.SCISSOR_BOX),re=i.getParameter(i.VIEWPORT),be=new vt().fromArray(Q),ee=new vt().fromArray(re);function oe(G,ge,ie,_e){let Me=new Uint8Array(4),ae=i.createTexture();i.bindTexture(G,ae),i.texParameteri(G,i.TEXTURE_MIN_FILTER,i.NEAREST),i.texParameteri(G,i.TEXTURE_MAG_FILTER,i.NEAREST);for(let Ne=0;Ne<ie;Ne++)G===i.TEXTURE_3D||G===i.TEXTURE_2D_ARRAY?i.texImage3D(ge,0,i.RGBA,1,1,_e,0,i.RGBA,i.UNSIGNED_BYTE,Me):i.texImage2D(ge+Ne,0,i.RGBA,1,1,0,i.RGBA,i.UNSIGNED_BYTE,Me);return ae}let V={};V[i.TEXTURE_2D]=oe(i.TEXTURE_2D,i.TEXTURE_2D,1),V[i.TEXTURE_CUBE_MAP]=oe(i.TEXTURE_CUBE_MAP,i.TEXTURE_CUBE_MAP_POSITIVE_X,6),V[i.TEXTURE_2D_ARRAY]=oe(i.TEXTURE_2D_ARRAY,i.TEXTURE_2D_ARRAY,1,1),V[i.TEXTURE_3D]=oe(i.TEXTURE_3D,i.TEXTURE_3D,1,1),o.setClear(0,0,0,1),a.setClear(1),l.setClear(0),Z(i.DEPTH_TEST),a.setFunc(Ss),Ge(!1),tt(_f),Z(i.CULL_FACE),$e(On);function Z(G){h[G]!==!0&&(i.enable(G),h[G]=!0)}function ce(G){h[G]!==!1&&(i.disable(G),h[G]=!1)}function Ee(G,ge){return f[G]!==ge?(i.bindFramebuffer(G,ge),f[G]=ge,G===i.DRAW_FRAMEBUFFER&&(f[i.FRAMEBUFFER]=ge),G===i.FRAMEBUFFER&&(f[i.DRAW_FRAMEBUFFER]=ge),!0):!1}function ue(G,ge){let ie=g,_e=!1;if(G){ie=p.get(ge),ie===void 0&&(ie=[],p.set(ge,ie));let Me=G.textures;if(ie.length!==Me.length||ie[0]!==i.COLOR_ATTACHMENT0){for(let ae=0,Ne=Me.length;ae<Ne;ae++)ie[ae]=i.COLOR_ATTACHMENT0+ae;ie.length=Me.length,_e=!0}}else ie[0]!==i.BACK&&(ie[0]=i.BACK,_e=!0);_e&&i.drawBuffers(ie)}function Be(G){return v!==G?(i.useProgram(G),v=G,!0):!1}let ut={[Ur]:i.FUNC_ADD,[qm]:i.FUNC_SUBTRACT,[jm]:i.FUNC_REVERSE_SUBTRACT};ut[Ym]=i.MIN,ut[$m]=i.MAX;let Ve={[Zm]:i.ZERO,[Km]:i.ONE,[Jm]:i.SRC_COLOR,[bf]:i.SRC_ALPHA,[rg]:i.SRC_ALPHA_SATURATE,[ng]:i.DST_COLOR,[eg]:i.DST_ALPHA,[Qm]:i.ONE_MINUS_SRC_COLOR,[Sf]:i.ONE_MINUS_SRC_ALPHA,[ig]:i.ONE_MINUS_DST_COLOR,[tg]:i.ONE_MINUS_DST_ALPHA,[sg]:i.CONSTANT_COLOR,[og]:i.ONE_MINUS_CONSTANT_COLOR,[ag]:i.CONSTANT_ALPHA,[lg]:i.ONE_MINUS_CONSTANT_ALPHA};function $e(G,ge,ie,_e,Me,ae,Ne,Ie,ft,nt){if(G===On){_===!0&&(ce(i.BLEND),_=!1);return}if(_===!1&&(Z(i.BLEND),_=!0),G!==Xm){if(G!==m||nt!==L){if((w!==Ur||b!==Ur)&&(i.blendEquation(i.FUNC_ADD),w=Ur,b=Ur),nt)switch(G){case ks:i.blendFuncSeparate(i.ONE,i.ONE_MINUS_SRC_ALPHA,i.ONE,i.ONE_MINUS_SRC_ALPHA);break;case vf:i.blendFunc(i.ONE,i.ONE);break;case yf:i.blendFuncSeparate(i.ZERO,i.ONE_MINUS_SRC_COLOR,i.ZERO,i.ONE);break;case xf:i.blendFuncSeparate(i.DST_COLOR,i.ONE_MINUS_SRC_ALPHA,i.ZERO,i.ONE);break;default:ze("WebGLState: Invalid blending: ",G);break}else switch(G){case ks:i.blendFuncSeparate(i.SRC_ALPHA,i.ONE_MINUS_SRC_ALPHA,i.ONE,i.ONE_MINUS_SRC_ALPHA);break;case vf:i.blendFuncSeparate(i.SRC_ALPHA,i.ONE,i.ONE,i.ONE);break;case yf:ze("WebGLState: SubtractiveBlending requires material.premultipliedAlpha = true");break;case xf:ze("WebGLState: MultiplyBlending requires material.premultipliedAlpha = true");break;default:ze("WebGLState: Invalid blending: ",G);break}T=null,y=null,S=null,A=null,x.set(0,0,0),C=0,m=G,L=nt}return}Me=Me||ge,ae=ae||ie,Ne=Ne||_e,(ge!==w||Me!==b)&&(i.blendEquationSeparate(ut[ge],ut[Me]),w=ge,b=Me),(ie!==T||_e!==y||ae!==S||Ne!==A)&&(i.blendFuncSeparate(Ve[ie],Ve[_e],Ve[ae],Ve[Ne]),T=ie,y=_e,S=ae,A=Ne),(Ie.equals(x)===!1||ft!==C)&&(i.blendColor(Ie.r,Ie.g,Ie.b,ft),x.copy(Ie),C=ft),m=G,L=!1}function Je(G,ge){G.side===di?ce(i.CULL_FACE):Z(i.CULL_FACE);let ie=G.side===Gt;ge&&(ie=!ie),Ge(ie),G.blending===ks&&G.transparent===!1?$e(On):$e(G.blending,G.blendEquation,G.blendSrc,G.blendDst,G.blendEquationAlpha,G.blendSrcAlpha,G.blendDstAlpha,G.blendColor,G.blendAlpha,G.premultipliedAlpha),a.setFunc(G.depthFunc),a.setTest(G.depthTest),a.setMask(G.depthWrite),o.setMask(G.colorWrite);let _e=G.stencilWrite;l.setTest(_e),_e&&(l.setMask(G.stencilWriteMask),l.setFunc(G.stencilFunc,G.stencilRef,G.stencilFuncMask),l.setOp(G.stencilFail,G.stencilZFail,G.stencilZPass)),fn(G.polygonOffset,G.polygonOffsetFactor,G.polygonOffsetUnits),G.alphaToCoverage===!0?Z(i.SAMPLE_ALPHA_TO_COVERAGE):ce(i.SAMPLE_ALPHA_TO_COVERAGE)}function Ge(G){P!==G&&(G?i.frontFace(i.CW):i.frontFace(i.CCW),P=G)}function tt(G){G!==Gm?(Z(i.CULL_FACE),G!==O&&(G===_f?i.cullFace(i.BACK):G===Hm?i.cullFace(i.FRONT):i.cullFace(i.FRONT_AND_BACK))):ce(i.CULL_FACE),O=G}function Dt(G){G!==U&&(k&&i.lineWidth(G),U=G)}function fn(G,ge,ie){G?(Z(i.POLYGON_OFFSET_FILL),(M!==ge||I!==ie)&&(M=ge,I=ie,a.getReversed()&&(ge=-ge),i.polygonOffset(ge,ie))):ce(i.POLYGON_OFFSET_FILL)}function St(G){G?Z(i.SCISSOR_TEST):ce(i.SCISSOR_TEST)}function At(G){G===void 0&&(G=i.TEXTURE0+D-1),j!==G&&(i.activeTexture(G),j=G)}function H(G,ge,ie){ie===void 0&&(j===null?ie=i.TEXTURE0+D-1:ie=j);let _e=B[ie];_e===void 0&&(_e={type:void 0,texture:void 0},B[ie]=_e),(_e.type!==G||_e.texture!==ge)&&(j!==ie&&(i.activeTexture(ie),j=ie),i.bindTexture(G,ge||V[G]),_e.type=G,_e.texture=ge)}function Xt(){let G=B[j];G!==void 0&&G.type!==void 0&&(i.bindTexture(G.type,null),G.type=void 0,G.texture=void 0)}function at(){try{i.compressedTexImage2D(...arguments)}catch(G){ze("WebGLState:",G)}}function N(){try{i.compressedTexImage3D(...arguments)}catch(G){ze("WebGLState:",G)}}function E(){try{i.texSubImage2D(...arguments)}catch(G){ze("WebGLState:",G)}}function X(){try{i.texSubImage3D(...arguments)}catch(G){ze("WebGLState:",G)}}function K(){try{i.compressedTexSubImage2D(...arguments)}catch(G){ze("WebGLState:",G)}}function te(){try{i.compressedTexSubImage3D(...arguments)}catch(G){ze("WebGLState:",G)}}function he(){try{i.texStorage2D(...arguments)}catch(G){ze("WebGLState:",G)}}function de(){try{i.texStorage3D(...arguments)}catch(G){ze("WebGLState:",G)}}function ne(){try{i.texImage2D(...arguments)}catch(G){ze("WebGLState:",G)}}function se(){try{i.texImage3D(...arguments)}catch(G){ze("WebGLState:",G)}}function pe(G){return d[G]!==void 0?d[G]:i.getParameter(G)}function De(G,ge){d[G]!==ge&&(i.pixelStorei(G,ge),d[G]=ge)}function ve(G){be.equals(G)===!1&&(i.scissor(G.x,G.y,G.z,G.w),be.copy(G))}function me(G){ee.equals(G)===!1&&(i.viewport(G.x,G.y,G.z,G.w),ee.copy(G))}function Oe(G,ge){let ie=u.get(ge);ie===void 0&&(ie=new WeakMap,u.set(ge,ie));let _e=ie.get(G);_e===void 0&&(_e=i.getUniformBlockIndex(ge,G.name),ie.set(G,_e))}function Fe(G,ge){let _e=u.get(ge).get(G);c.get(ge)!==_e&&(i.uniformBlockBinding(ge,_e,G.__bindingPointIndex),c.set(ge,_e))}function Xe(){i.disable(i.BLEND),i.disable(i.CULL_FACE),i.disable(i.DEPTH_TEST),i.disable(i.POLYGON_OFFSET_FILL),i.disable(i.SCISSOR_TEST),i.disable(i.STENCIL_TEST),i.disable(i.SAMPLE_ALPHA_TO_COVERAGE),i.blendEquation(i.FUNC_ADD),i.blendFunc(i.ONE,i.ZERO),i.blendFuncSeparate(i.ONE,i.ZERO,i.ONE,i.ZERO),i.blendColor(0,0,0,0),i.colorMask(!0,!0,!0,!0),i.clearColor(0,0,0,0),i.depthMask(!0),i.depthFunc(i.LESS),a.setReversed(!1),i.clearDepth(1),i.stencilMask(4294967295),i.stencilFunc(i.ALWAYS,0,4294967295),i.stencilOp(i.KEEP,i.KEEP,i.KEEP),i.clearStencil(0),i.cullFace(i.BACK),i.frontFace(i.CCW),i.polygonOffset(0,0),i.activeTexture(i.TEXTURE0),i.bindFramebuffer(i.FRAMEBUFFER,null),i.bindFramebuffer(i.DRAW_FRAMEBUFFER,null),i.bindFramebuffer(i.READ_FRAMEBUFFER,null),i.useProgram(null),i.lineWidth(1),i.scissor(0,0,i.canvas.width,i.canvas.height),i.viewport(0,0,i.canvas.width,i.canvas.height),i.pixelStorei(i.PACK_ALIGNMENT,4),i.pixelStorei(i.UNPACK_ALIGNMENT,4),i.pixelStorei(i.UNPACK_FLIP_Y_WEBGL,!1),i.pixelStorei(i.UNPACK_PREMULTIPLY_ALPHA_WEBGL,!1),i.pixelStorei(i.UNPACK_COLORSPACE_CONVERSION_WEBGL,i.BROWSER_DEFAULT_WEBGL),i.pixelStorei(i.PACK_ROW_LENGTH,0),i.pixelStorei(i.PACK_SKIP_PIXELS,0),i.pixelStorei(i.PACK_SKIP_ROWS,0),i.pixelStorei(i.UNPACK_ROW_LENGTH,0),i.pixelStorei(i.UNPACK_IMAGE_HEIGHT,0),i.pixelStorei(i.UNPACK_SKIP_PIXELS,0),i.pixelStorei(i.UNPACK_SKIP_ROWS,0),i.pixelStorei(i.UNPACK_SKIP_IMAGES,0),h={},d={},j=null,B={},f={},p=new WeakMap,g=[],v=null,_=!1,m=null,w=null,T=null,y=null,b=null,S=null,A=null,x=new We(0,0,0),C=0,L=!1,P=null,O=null,U=null,M=null,I=null,be.set(0,0,i.canvas.width,i.canvas.height),ee.set(0,0,i.canvas.width,i.canvas.height),o.reset(),a.reset(),l.reset()}return{buffers:{color:o,depth:a,stencil:l},enable:Z,disable:ce,bindFramebuffer:Ee,drawBuffers:ue,useProgram:Be,setBlending:$e,setMaterial:Je,setFlipSided:Ge,setCullFace:tt,setLineWidth:Dt,setPolygonOffset:fn,setScissorTest:St,activeTexture:At,bindTexture:H,unbindTexture:Xt,compressedTexImage2D:at,compressedTexImage3D:N,texImage2D:ne,texImage3D:se,pixelStorei:De,getParameter:pe,updateUBOMapping:Oe,uniformBlockBinding:Fe,texStorage2D:he,texStorage3D:de,texSubImage2D:E,texSubImage3D:X,compressedTexSubImage2D:K,compressedTexSubImage3D:te,scissor:ve,viewport:me,reset:Xe}}function qA(i,e,n,r,s,o,a){let l=e.has("WEBGL_multisampled_render_to_texture")?e.get("WEBGL_multisampled_render_to_texture"):null,c=typeof navigator>"u"?!1:/OculusBrowser/g.test(navigator.userAgent),u=new fe,h=new WeakMap,d=new Set,f,p=new WeakMap,g=!1;try{g=typeof OffscreenCanvas<"u"&&new OffscreenCanvas(1,1).getContext("2d")!==null}catch{}function v(N,E){return g?new OffscreenCanvas(N,E):Ms("canvas")}function _(N,E,X){let K=1,te=at(N);if((te.width>X||te.height>X)&&(K=X/Math.max(te.width,te.height)),K<1)if(typeof HTMLImageElement<"u"&&N instanceof HTMLImageElement||typeof HTMLCanvasElement<"u"&&N instanceof HTMLCanvasElement||typeof ImageBitmap<"u"&&N instanceof ImageBitmap||typeof VideoFrame<"u"&&N instanceof VideoFrame){let he=Math.floor(K*te.width),de=Math.floor(K*te.height);f===void 0&&(f=v(he,de));let ne=E?v(he,de):f;return ne.width=he,ne.height=de,ne.getContext("2d").drawImage(N,0,0,he,de),Ue("WebGLRenderer: Texture has been resized from ("+te.width+"x"+te.height+") to ("+he+"x"+de+")."),ne}else return"data"in N&&Ue("WebGLRenderer: Image in DataTexture is too big ("+te.width+"x"+te.height+")."),N;return N}function m(N){return N.generateMipmaps}function w(N){i.generateMipmap(N)}function T(N){return N.isWebGLCubeRenderTarget?i.TEXTURE_CUBE_MAP:N.isWebGL3DRenderTarget?i.TEXTURE_3D:N.isWebGLArrayRenderTarget||N.isCompressedArrayTexture?i.TEXTURE_2D_ARRAY:i.TEXTURE_2D}function y(N,E,X,K,te,he=!1){if(N!==null){if(i[N]!==void 0)return i[N];Ue("WebGLRenderer: Attempt to use non-existing WebGL internal format '"+N+"'")}let de;K&&(de=e.get("EXT_texture_norm16"),de||Ue("WebGLRenderer: Unable to use normalized textures without EXT_texture_norm16 extension"));let ne=E;if(E===i.RED&&(X===i.FLOAT&&(ne=i.R32F),X===i.HALF_FLOAT&&(ne=i.R16F),X===i.UNSIGNED_BYTE&&(ne=i.R8),X===i.UNSIGNED_SHORT&&de&&(ne=de.R16_EXT),X===i.SHORT&&de&&(ne=de.R16_SNORM_EXT)),E===i.RED_INTEGER&&(X===i.UNSIGNED_BYTE&&(ne=i.R8UI),X===i.UNSIGNED_SHORT&&(ne=i.R16UI),X===i.UNSIGNED_INT&&(ne=i.R32UI),X===i.BYTE&&(ne=i.R8I),X===i.SHORT&&(ne=i.R16I),X===i.INT&&(ne=i.R32I)),E===i.RG&&(X===i.FLOAT&&(ne=i.RG32F),X===i.HALF_FLOAT&&(ne=i.RG16F),X===i.UNSIGNED_BYTE&&(ne=i.RG8),X===i.UNSIGNED_SHORT&&de&&(ne=de.RG16_EXT),X===i.SHORT&&de&&(ne=de.RG16_SNORM_EXT)),E===i.RG_INTEGER&&(X===i.UNSIGNED_BYTE&&(ne=i.RG8UI),X===i.UNSIGNED_SHORT&&(ne=i.RG16UI),X===i.UNSIGNED_INT&&(ne=i.RG32UI),X===i.BYTE&&(ne=i.RG8I),X===i.SHORT&&(ne=i.RG16I),X===i.INT&&(ne=i.RG32I)),E===i.RGB_INTEGER&&(X===i.UNSIGNED_BYTE&&(ne=i.RGB8UI),X===i.UNSIGNED_SHORT&&(ne=i.RGB16UI),X===i.UNSIGNED_INT&&(ne=i.RGB32UI),X===i.BYTE&&(ne=i.RGB8I),X===i.SHORT&&(ne=i.RGB16I),X===i.INT&&(ne=i.RGB32I)),E===i.RGBA_INTEGER&&(X===i.UNSIGNED_BYTE&&(ne=i.RGBA8UI),X===i.UNSIGNED_SHORT&&(ne=i.RGBA16UI),X===i.UNSIGNED_INT&&(ne=i.RGBA32UI),X===i.BYTE&&(ne=i.RGBA8I),X===i.SHORT&&(ne=i.RGBA16I),X===i.INT&&(ne=i.RGBA32I)),E===i.RGB&&(X===i.UNSIGNED_SHORT&&de&&(ne=de.RGB16_EXT),X===i.SHORT&&de&&(ne=de.RGB16_SNORM_EXT),X===i.UNSIGNED_INT_5_9_9_9_REV&&(ne=i.RGB9_E5),X===i.UNSIGNED_INT_10F_11F_11F_REV&&(ne=i.R11F_G11F_B10F)),E===i.RGBA){let se=he?Mo:Ke.getTransfer(te);X===i.FLOAT&&(ne=i.RGBA32F),X===i.HALF_FLOAT&&(ne=i.RGBA16F),X===i.UNSIGNED_BYTE&&(ne=se===rt?i.SRGB8_ALPHA8:i.RGBA8),X===i.UNSIGNED_SHORT&&de&&(ne=de.RGBA16_EXT),X===i.SHORT&&de&&(ne=de.RGBA16_SNORM_EXT),X===i.UNSIGNED_SHORT_4_4_4_4&&(ne=i.RGBA4),X===i.UNSIGNED_SHORT_5_5_5_1&&(ne=i.RGB5_A1)}return(ne===i.R16F||ne===i.R32F||ne===i.RG16F||ne===i.RG32F||ne===i.RGBA16F||ne===i.RGBA32F)&&e.get("EXT_color_buffer_float"),ne}function b(N,E){let X;return N?E===null||E===Kn||E===zs?X=i.DEPTH24_STENCIL8:E===Jn?X=i.DEPTH32F_STENCIL8:E===Bs&&(X=i.DEPTH24_STENCIL8,Ue("DepthTexture: 16 bit depth attachment is not supported with stencil. Using 24-bit attachment.")):E===null||E===Kn||E===zs?X=i.DEPTH_COMPONENT24:E===Jn?X=i.DEPTH_COMPONENT32F:E===Bs&&(X=i.DEPTH_COMPONENT16),X}function S(N,E){return m(N)===!0||N.isFramebufferTexture&&N.minFilter!==Nt&&N.minFilter!==Vt?Math.log2(Math.max(E.width,E.height))+1:N.mipmaps!==void 0&&N.mipmaps.length>0?N.mipmaps.length:N.isCompressedTexture&&Array.isArray(N.image)?E.mipmaps.length:1}function A(N){let E=N.target;E.removeEventListener("dispose",A),C(E),E.isVideoTexture&&h.delete(E),E.isHTMLTexture&&d.delete(E)}function x(N){let E=N.target;E.removeEventListener("dispose",x),P(E)}function C(N){let E=r.get(N);if(E.__webglInit===void 0)return;let X=N.source,K=p.get(X);if(K){let te=K[E.__cacheKey];te.usedTimes--,te.usedTimes===0&&L(N),Object.keys(K).length===0&&p.delete(X)}r.remove(N)}function L(N){let E=r.get(N);i.deleteTexture(E.__webglTexture);let X=N.source,K=p.get(X);delete K[E.__cacheKey],a.memory.textures--}function P(N){let E=r.get(N);if(N.depthTexture&&(N.depthTexture.dispose(),r.remove(N.depthTexture)),N.isWebGLCubeRenderTarget)for(let K=0;K<6;K++){if(Array.isArray(E.__webglFramebuffer[K]))for(let te=0;te<E.__webglFramebuffer[K].length;te++)i.deleteFramebuffer(E.__webglFramebuffer[K][te]);else i.deleteFramebuffer(E.__webglFramebuffer[K]);E.__webglDepthbuffer&&i.deleteRenderbuffer(E.__webglDepthbuffer[K])}else{if(Array.isArray(E.__webglFramebuffer))for(let K=0;K<E.__webglFramebuffer.length;K++)i.deleteFramebuffer(E.__webglFramebuffer[K]);else i.deleteFramebuffer(E.__webglFramebuffer);if(E.__webglDepthbuffer&&i.deleteRenderbuffer(E.__webglDepthbuffer),E.__webglMultisampledFramebuffer&&i.deleteFramebuffer(E.__webglMultisampledFramebuffer),E.__webglColorRenderbuffer)for(let K=0;K<E.__webglColorRenderbuffer.length;K++)E.__webglColorRenderbuffer[K]&&i.deleteRenderbuffer(E.__webglColorRenderbuffer[K]);E.__webglDepthRenderbuffer&&i.deleteRenderbuffer(E.__webglDepthRenderbuffer)}let X=N.textures;for(let K=0,te=X.length;K<te;K++){let he=r.get(X[K]);he.__webglTexture&&(i.deleteTexture(he.__webglTexture),a.memory.textures--),r.remove(X[K])}r.remove(N)}let O=0;function U(){O=0}function M(){return O}function I(N){O=N}function D(){let N=O;return N>=s.maxTextures&&Ue("WebGLTextures: Trying to use "+(N+1)+" texture units while this GPU supports only "+s.maxTextures),O+=1,N}function k(N){let E=[];return E.push(N.wrapS),E.push(N.wrapT),E.push(N.wrapR||0),E.push(N.magFilter),E.push(N.minFilter),E.push(N.anisotropy),E.push(N.internalFormat),E.push(N.format),E.push(N.type),E.push(N.generateMipmaps),E.push(N.premultiplyAlpha),E.push(N.flipY),E.push(N.unpackAlignment),E.push(N.colorSpace),E.join()}function q(N,E){let X=r.get(N);if(N.isVideoTexture&&H(N),N.isRenderTargetTexture===!1&&N.isExternalTexture!==!0&&N.version>0&&X.__version!==N.version){let K=N.image;if(K===null)Ue("WebGLRenderer: Texture marked for update but no image data found.");else if(K.complete===!1)Ue("WebGLRenderer: Texture marked for update but image is incomplete");else{ce(X,N,E);return}}else N.isExternalTexture&&(X.__webglTexture=N.sourceTexture?N.sourceTexture:null);n.bindTexture(i.TEXTURE_2D,X.__webglTexture,i.TEXTURE0+E)}function W(N,E){let X=r.get(N);if(N.isRenderTargetTexture===!1&&N.version>0&&X.__version!==N.version){ce(X,N,E);return}else N.isExternalTexture&&(X.__webglTexture=N.sourceTexture?N.sourceTexture:null);n.bindTexture(i.TEXTURE_2D_ARRAY,X.__webglTexture,i.TEXTURE0+E)}function j(N,E){let X=r.get(N);if(N.isRenderTargetTexture===!1&&N.version>0&&X.__version!==N.version){ce(X,N,E);return}n.bindTexture(i.TEXTURE_3D,X.__webglTexture,i.TEXTURE0+E)}function B(N,E){let X=r.get(N);if(N.isCubeDepthTexture!==!0&&N.version>0&&X.__version!==N.version){Ee(X,N,E);return}n.bindTexture(i.TEXTURE_CUBE_MAP,X.__webglTexture,i.TEXTURE0+E)}let Q={[Il]:i.REPEAT,[ui]:i.CLAMP_TO_EDGE,[Ll]:i.MIRRORED_REPEAT},re={[Nt]:i.NEAREST,[hg]:i.NEAREST_MIPMAP_NEAREST,[Xo]:i.NEAREST_MIPMAP_LINEAR,[Vt]:i.LINEAR,[pc]:i.LINEAR_MIPMAP_NEAREST,[sr]:i.LINEAR_MIPMAP_LINEAR},be={[mg]:i.NEVER,[xg]:i.ALWAYS,[gg]:i.LESS,[Jc]:i.LEQUAL,[_g]:i.EQUAL,[Qc]:i.GEQUAL,[vg]:i.GREATER,[yg]:i.NOTEQUAL};function ee(N,E){if(E.type===Jn&&e.has("OES_texture_float_linear")===!1&&(E.magFilter===Vt||E.magFilter===pc||E.magFilter===Xo||E.magFilter===sr||E.minFilter===Vt||E.minFilter===pc||E.minFilter===Xo||E.minFilter===sr)&&Ue("WebGLRenderer: Unable to use linear filtering with floating point textures. OES_texture_float_linear not supported on this device."),i.texParameteri(N,i.TEXTURE_WRAP_S,Q[E.wrapS]),i.texParameteri(N,i.TEXTURE_WRAP_T,Q[E.wrapT]),(N===i.TEXTURE_3D||N===i.TEXTURE_2D_ARRAY)&&i.texParameteri(N,i.TEXTURE_WRAP_R,Q[E.wrapR]),i.texParameteri(N,i.TEXTURE_MAG_FILTER,re[E.magFilter]),i.texParameteri(N,i.TEXTURE_MIN_FILTER,re[E.minFilter]),E.compareFunction&&(i.texParameteri(N,i.TEXTURE_COMPARE_MODE,i.COMPARE_REF_TO_TEXTURE),i.texParameteri(N,i.TEXTURE_COMPARE_FUNC,be[E.compareFunction])),e.has("EXT_texture_filter_anisotropic")===!0){if(E.magFilter===Nt||E.minFilter!==Xo&&E.minFilter!==sr||E.type===Jn&&e.has("OES_texture_float_linear")===!1)return;if(E.anisotropy>1||r.get(E).__currentAnisotropy){let X=e.get("EXT_texture_filter_anisotropic");i.texParameterf(N,X.TEXTURE_MAX_ANISOTROPY_EXT,Math.min(E.anisotropy,s.getMaxAnisotropy())),r.get(E).__currentAnisotropy=E.anisotropy}}}function oe(N,E){let X=!1;N.__webglInit===void 0&&(N.__webglInit=!0,E.addEventListener("dispose",A));let K=E.source,te=p.get(K);te===void 0&&(te={},p.set(K,te));let he=k(E);if(he!==N.__cacheKey){te[he]===void 0&&(te[he]={texture:i.createTexture(),usedTimes:0},a.memory.textures++,X=!0),te[he].usedTimes++;let de=te[N.__cacheKey];de!==void 0&&(te[N.__cacheKey].usedTimes--,de.usedTimes===0&&L(E)),N.__cacheKey=he,N.__webglTexture=te[he].texture}return X}function V(N,E,X){return Math.floor(Math.floor(N/X)/E)}function Z(N,E,X,K){let he=N.updateRanges;if(he.length===0)n.texSubImage2D(i.TEXTURE_2D,0,0,0,E.width,E.height,X,K,E.data);else{he.sort((De,ve)=>De.start-ve.start);let de=0;for(let De=1;De<he.length;De++){let ve=he[de],me=he[De],Oe=ve.start+ve.count,Fe=V(me.start,E.width,4),Xe=V(ve.start,E.width,4);me.start<=Oe+1&&Fe===Xe&&V(me.start+me.count-1,E.width,4)===Fe?ve.count=Math.max(ve.count,me.start+me.count-ve.start):(++de,he[de]=me)}he.length=de+1;let ne=n.getParameter(i.UNPACK_ROW_LENGTH),se=n.getParameter(i.UNPACK_SKIP_PIXELS),pe=n.getParameter(i.UNPACK_SKIP_ROWS);n.pixelStorei(i.UNPACK_ROW_LENGTH,E.width);for(let De=0,ve=he.length;De<ve;De++){let me=he[De],Oe=Math.floor(me.start/4),Fe=Math.ceil(me.count/4),Xe=Oe%E.width,G=Math.floor(Oe/E.width),ge=Fe,ie=1;n.pixelStorei(i.UNPACK_SKIP_PIXELS,Xe),n.pixelStorei(i.UNPACK_SKIP_ROWS,G),n.texSubImage2D(i.TEXTURE_2D,0,Xe,G,ge,ie,X,K,E.data)}N.clearUpdateRanges(),n.pixelStorei(i.UNPACK_ROW_LENGTH,ne),n.pixelStorei(i.UNPACK_SKIP_PIXELS,se),n.pixelStorei(i.UNPACK_SKIP_ROWS,pe)}}function ce(N,E,X){let K=i.TEXTURE_2D;(E.isDataArrayTexture||E.isCompressedArrayTexture)&&(K=i.TEXTURE_2D_ARRAY),E.isData3DTexture&&(K=i.TEXTURE_3D);let te=oe(N,E),he=E.source;n.bindTexture(K,N.__webglTexture,i.TEXTURE0+X);let de=r.get(he);if(he.version!==de.__version||te===!0){if(n.activeTexture(i.TEXTURE0+X),(typeof ImageBitmap<"u"&&E.image instanceof ImageBitmap)===!1){let ie=Ke.getPrimaries(Ke.workingColorSpace),_e=E.colorSpace===Pi?null:Ke.getPrimaries(E.colorSpace),Me=E.colorSpace===Pi||ie===_e?i.NONE:i.BROWSER_DEFAULT_WEBGL;n.pixelStorei(i.UNPACK_FLIP_Y_WEBGL,E.flipY),n.pixelStorei(i.UNPACK_PREMULTIPLY_ALPHA_WEBGL,E.premultiplyAlpha),n.pixelStorei(i.UNPACK_COLORSPACE_CONVERSION_WEBGL,Me)}n.pixelStorei(i.UNPACK_ALIGNMENT,E.unpackAlignment);let se=_(E.image,!1,s.maxTextureSize);se=Xt(E,se);let pe=o.convert(E.format,E.colorSpace),De=o.convert(E.type),ve=y(E.internalFormat,pe,De,E.normalized,E.colorSpace,E.isVideoTexture);ee(K,E);let me,Oe=E.mipmaps,Fe=E.isVideoTexture!==!0,Xe=de.__version===void 0||te===!0,G=he.dataReady,ge=S(E,se);if(E.isDepthTexture)ve=b(E.format===or,E.type),Xe&&(Fe?n.texStorage2D(i.TEXTURE_2D,1,ve,se.width,se.height):n.texImage2D(i.TEXTURE_2D,0,ve,se.width,se.height,0,pe,De,null));else if(E.isDataTexture)if(Oe.length>0){Fe&&Xe&&n.texStorage2D(i.TEXTURE_2D,ge,ve,Oe[0].width,Oe[0].height);for(let ie=0,_e=Oe.length;ie<_e;ie++)me=Oe[ie],Fe?G&&n.texSubImage2D(i.TEXTURE_2D,ie,0,0,me.width,me.height,pe,De,me.data):n.texImage2D(i.TEXTURE_2D,ie,ve,me.width,me.height,0,pe,De,me.data);E.generateMipmaps=!1}else Fe?(Xe&&n.texStorage2D(i.TEXTURE_2D,ge,ve,se.width,se.height),G&&Z(E,se,pe,De)):n.texImage2D(i.TEXTURE_2D,0,ve,se.width,se.height,0,pe,De,se.data);else if(E.isCompressedTexture)if(E.isCompressedArrayTexture){Fe&&Xe&&n.texStorage3D(i.TEXTURE_2D_ARRAY,ge,ve,Oe[0].width,Oe[0].height,se.depth);for(let ie=0,_e=Oe.length;ie<_e;ie++)if(me=Oe[ie],E.format!==Nn)if(pe!==null)if(Fe){if(G)if(E.layerUpdates.size>0){let Me=Xf(me.width,me.height,E.format,E.type);for(let ae of E.layerUpdates){let Ne=me.data.subarray(ae*Me/me.data.BYTES_PER_ELEMENT,(ae+1)*Me/me.data.BYTES_PER_ELEMENT);n.compressedTexSubImage3D(i.TEXTURE_2D_ARRAY,ie,0,0,ae,me.width,me.height,1,pe,Ne)}}else n.compressedTexSubImage3D(i.TEXTURE_2D_ARRAY,ie,0,0,0,me.width,me.height,se.depth,pe,me.data)}else n.compressedTexImage3D(i.TEXTURE_2D_ARRAY,ie,ve,me.width,me.height,se.depth,0,me.data,0,0);else Ue("WebGLRenderer: Attempt to load unsupported compressed texture format in .uploadTexture()");else Fe?G&&n.texSubImage3D(i.TEXTURE_2D_ARRAY,ie,0,0,0,me.width,me.height,se.depth,pe,De,me.data):n.texImage3D(i.TEXTURE_2D_ARRAY,ie,ve,me.width,me.height,se.depth,0,pe,De,me.data);E.layerUpdates.size>0&&E.clearLayerUpdates()}else{Fe&&Xe&&n.texStorage2D(i.TEXTURE_2D,ge,ve,Oe[0].width,Oe[0].height);for(let ie=0,_e=Oe.length;ie<_e;ie++)me=Oe[ie],E.format!==Nn?pe!==null?Fe?G&&n.compressedTexSubImage2D(i.TEXTURE_2D,ie,0,0,me.width,me.height,pe,me.data):n.compressedTexImage2D(i.TEXTURE_2D,ie,ve,me.width,me.height,0,me.data):Ue("WebGLRenderer: Attempt to load unsupported compressed texture format in .uploadTexture()"):Fe?G&&n.texSubImage2D(i.TEXTURE_2D,ie,0,0,me.width,me.height,pe,De,me.data):n.texImage2D(i.TEXTURE_2D,ie,ve,me.width,me.height,0,pe,De,me.data)}else if(E.isDataArrayTexture)if(Fe){if(Xe&&n.texStorage3D(i.TEXTURE_2D_ARRAY,ge,ve,se.width,se.height,se.depth),G)if(E.layerUpdates.size>0){let ie=Xf(se.width,se.height,E.format,E.type);for(let _e of E.layerUpdates){let Me=se.data.subarray(_e*ie/se.data.BYTES_PER_ELEMENT,(_e+1)*ie/se.data.BYTES_PER_ELEMENT);n.texSubImage3D(i.TEXTURE_2D_ARRAY,0,0,0,_e,se.width,se.height,1,pe,De,Me)}E.clearLayerUpdates()}else n.texSubImage3D(i.TEXTURE_2D_ARRAY,0,0,0,0,se.width,se.height,se.depth,pe,De,se.data)}else n.texImage3D(i.TEXTURE_2D_ARRAY,0,ve,se.width,se.height,se.depth,0,pe,De,se.data);else if(E.isData3DTexture)Fe?(Xe&&n.texStorage3D(i.TEXTURE_3D,ge,ve,se.width,se.height,se.depth),G&&n.texSubImage3D(i.TEXTURE_3D,0,0,0,0,se.width,se.height,se.depth,pe,De,se.data)):n.texImage3D(i.TEXTURE_3D,0,ve,se.width,se.height,se.depth,0,pe,De,se.data);else if(E.isFramebufferTexture){if(Xe)if(Fe)n.texStorage2D(i.TEXTURE_2D,ge,ve,se.width,se.height);else{let ie=se.width,_e=se.height;for(let Me=0;Me<ge;Me++)n.texImage2D(i.TEXTURE_2D,Me,ve,ie,_e,0,pe,De,null),ie>>=1,_e>>=1}}else if(E.isHTMLTexture){if("texElementImage2D"in i){let ie=i.canvas;if(ie.hasAttribute("layoutsubtree")||ie.setAttribute("layoutsubtree","true"),se.parentNode!==ie){ie.appendChild(se),d.add(E),ie.onpaint=_e=>{let Me=_e.changedElements;for(let ae of d)Me.includes(ae.image)&&(ae.needsUpdate=!0)},ie.requestPaint();return}if(i.texElementImage2D.length===3)i.texElementImage2D(i.TEXTURE_2D,i.RGBA8,se);else{let Me=i.RGBA,ae=i.RGBA,Ne=i.UNSIGNED_BYTE;i.texElementImage2D(i.TEXTURE_2D,0,Me,ae,Ne,se)}i.texParameteri(i.TEXTURE_2D,i.TEXTURE_MIN_FILTER,i.LINEAR),i.texParameteri(i.TEXTURE_2D,i.TEXTURE_WRAP_S,i.CLAMP_TO_EDGE),i.texParameteri(i.TEXTURE_2D,i.TEXTURE_WRAP_T,i.CLAMP_TO_EDGE)}}else if(Oe.length>0){if(Fe&&Xe){let ie=at(Oe[0]);n.texStorage2D(i.TEXTURE_2D,ge,ve,ie.width,ie.height)}for(let ie=0,_e=Oe.length;ie<_e;ie++)me=Oe[ie],Fe?G&&n.texSubImage2D(i.TEXTURE_2D,ie,0,0,pe,De,me):n.texImage2D(i.TEXTURE_2D,ie,ve,pe,De,me);E.generateMipmaps=!1}else if(Fe){if(Xe){let ie=at(se);n.texStorage2D(i.TEXTURE_2D,ge,ve,ie.width,ie.height)}G&&n.texSubImage2D(i.TEXTURE_2D,0,0,0,pe,De,se)}else n.texImage2D(i.TEXTURE_2D,0,ve,pe,De,se);m(E)&&w(K),de.__version=he.version,E.onUpdate&&E.onUpdate(E)}N.__version=E.version}function Ee(N,E,X){if(E.image.length!==6)return;let K=oe(N,E),te=E.source;n.bindTexture(i.TEXTURE_CUBE_MAP,N.__webglTexture,i.TEXTURE0+X);let he=r.get(te);if(te.version!==he.__version||K===!0){n.activeTexture(i.TEXTURE0+X);let de=Ke.getPrimaries(Ke.workingColorSpace),ne=E.colorSpace===Pi?null:Ke.getPrimaries(E.colorSpace),se=E.colorSpace===Pi||de===ne?i.NONE:i.BROWSER_DEFAULT_WEBGL;n.pixelStorei(i.UNPACK_FLIP_Y_WEBGL,E.flipY),n.pixelStorei(i.UNPACK_PREMULTIPLY_ALPHA_WEBGL,E.premultiplyAlpha),n.pixelStorei(i.UNPACK_ALIGNMENT,E.unpackAlignment),n.pixelStorei(i.UNPACK_COLORSPACE_CONVERSION_WEBGL,se);let pe=E.isCompressedTexture||E.image[0].isCompressedTexture,De=E.image[0]&&E.image[0].isDataTexture,ve=[];for(let ae=0;ae<6;ae++)!pe&&!De?ve[ae]=_(E.image[ae],!0,s.maxCubemapSize):ve[ae]=De?E.image[ae].image:E.image[ae],ve[ae]=Xt(E,ve[ae]);let me=ve[0],Oe=o.convert(E.format,E.colorSpace),Fe=o.convert(E.type),Xe=y(E.internalFormat,Oe,Fe,E.normalized,E.colorSpace),G=E.isVideoTexture!==!0,ge=he.__version===void 0||K===!0,ie=te.dataReady,_e=S(E,me);ee(i.TEXTURE_CUBE_MAP,E);let Me;if(pe){G&&ge&&n.texStorage2D(i.TEXTURE_CUBE_MAP,_e,Xe,me.width,me.height);for(let ae=0;ae<6;ae++){Me=ve[ae].mipmaps;for(let Ne=0;Ne<Me.length;Ne++){let Ie=Me[Ne];E.format!==Nn?Oe!==null?G?ie&&n.compressedTexSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne,0,0,Ie.width,Ie.height,Oe,Ie.data):n.compressedTexImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne,Xe,Ie.width,Ie.height,0,Ie.data):Ue("WebGLRenderer: Attempt to load unsupported compressed texture format in .setTextureCube()"):G?ie&&n.texSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne,0,0,Ie.width,Ie.height,Oe,Fe,Ie.data):n.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne,Xe,Ie.width,Ie.height,0,Oe,Fe,Ie.data)}}}else{if(Me=E.mipmaps,G&&ge){Me.length>0&&_e++;let ae=at(ve[0]);n.texStorage2D(i.TEXTURE_CUBE_MAP,_e,Xe,ae.width,ae.height)}for(let ae=0;ae<6;ae++)if(De){G?ie&&n.texSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,0,0,0,ve[ae].width,ve[ae].height,Oe,Fe,ve[ae].data):n.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,0,Xe,ve[ae].width,ve[ae].height,0,Oe,Fe,ve[ae].data);for(let Ne=0;Ne<Me.length;Ne++){let ft=Me[Ne].image[ae].image;G?ie&&n.texSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne+1,0,0,ft.width,ft.height,Oe,Fe,ft.data):n.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne+1,Xe,ft.width,ft.height,0,Oe,Fe,ft.data)}}else{G?ie&&n.texSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,0,0,0,Oe,Fe,ve[ae]):n.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,0,Xe,Oe,Fe,ve[ae]);for(let Ne=0;Ne<Me.length;Ne++){let Ie=Me[Ne];G?ie&&n.texSubImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne+1,0,0,Oe,Fe,Ie.image[ae]):n.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+ae,Ne+1,Xe,Oe,Fe,Ie.image[ae])}}}m(E)&&w(i.TEXTURE_CUBE_MAP),he.__version=te.version,E.onUpdate&&E.onUpdate(E)}N.__version=E.version}function ue(N,E,X,K,te,he){let de=o.convert(X.format,X.colorSpace),ne=o.convert(X.type),se=y(X.internalFormat,de,ne,X.normalized,X.colorSpace),pe=r.get(E),De=r.get(X);if(De.__renderTarget=E,!pe.__hasExternalTextures){let ve=Math.max(1,E.width>>he),me=Math.max(1,E.height>>he);te===i.TEXTURE_3D||te===i.TEXTURE_2D_ARRAY?n.texImage3D(te,he,se,ve,me,E.depth,0,de,ne,null):n.texImage2D(te,he,se,ve,me,0,de,ne,null)}n.bindFramebuffer(i.FRAMEBUFFER,N),At(E)?l.framebufferTexture2DMultisampleEXT(i.FRAMEBUFFER,K,te,De.__webglTexture,0,St(E)):(te===i.TEXTURE_2D||te>=i.TEXTURE_CUBE_MAP_POSITIVE_X&&te<=i.TEXTURE_CUBE_MAP_NEGATIVE_Z)&&i.framebufferTexture2D(i.FRAMEBUFFER,K,te,De.__webglTexture,he),n.bindFramebuffer(i.FRAMEBUFFER,null)}function Be(N,E,X){if(i.bindRenderbuffer(i.RENDERBUFFER,N),E.depthBuffer){let K=E.depthTexture,te=K&&K.isDepthTexture?K.type:null,he=b(E.stencilBuffer,te),de=E.stencilBuffer?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT;At(E)?l.renderbufferStorageMultisampleEXT(i.RENDERBUFFER,St(E),he,E.width,E.height):X?i.renderbufferStorageMultisample(i.RENDERBUFFER,St(E),he,E.width,E.height):i.renderbufferStorage(i.RENDERBUFFER,he,E.width,E.height),i.framebufferRenderbuffer(i.FRAMEBUFFER,de,i.RENDERBUFFER,N)}else{let K=E.textures;for(let te=0;te<K.length;te++){let he=K[te],de=o.convert(he.format,he.colorSpace),ne=o.convert(he.type),se=y(he.internalFormat,de,ne,he.normalized,he.colorSpace);At(E)?l.renderbufferStorageMultisampleEXT(i.RENDERBUFFER,St(E),se,E.width,E.height):X?i.renderbufferStorageMultisample(i.RENDERBUFFER,St(E),se,E.width,E.height):i.renderbufferStorage(i.RENDERBUFFER,se,E.width,E.height)}}i.bindRenderbuffer(i.RENDERBUFFER,null)}function ut(N,E,X){let K=E.isWebGLCubeRenderTarget===!0;if(n.bindFramebuffer(i.FRAMEBUFFER,N),!(E.depthTexture&&E.depthTexture.isDepthTexture))throw new Error("THREE.WebGLTextures: renderTarget.depthTexture must be an instance of THREE.DepthTexture.");let te=r.get(E.depthTexture);if(te.__renderTarget=E,(!te.__webglTexture||E.depthTexture.image.width!==E.width||E.depthTexture.image.height!==E.height)&&(E.depthTexture.image.width=E.width,E.depthTexture.image.height=E.height,E.depthTexture.needsUpdate=!0),K){if(te.__webglInit===void 0&&(te.__webglInit=!0,E.depthTexture.addEventListener("dispose",A)),te.__webglTexture===void 0){te.__webglTexture=i.createTexture(),n.bindTexture(i.TEXTURE_CUBE_MAP,te.__webglTexture),ee(i.TEXTURE_CUBE_MAP,E.depthTexture);let pe=o.convert(E.depthTexture.format),De=o.convert(E.depthTexture.type),ve;E.depthTexture.format===fi?ve=i.DEPTH_COMPONENT24:E.depthTexture.format===or&&(ve=i.DEPTH24_STENCIL8);for(let me=0;me<6;me++)i.texImage2D(i.TEXTURE_CUBE_MAP_POSITIVE_X+me,0,ve,E.width,E.height,0,pe,De,null)}}else q(E.depthTexture,0);let he=te.__webglTexture,de=St(E),ne=K?i.TEXTURE_CUBE_MAP_POSITIVE_X+X:i.TEXTURE_2D,se=E.depthTexture.format===or?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT;if(E.depthTexture.format===fi)At(E)?l.framebufferTexture2DMultisampleEXT(i.FRAMEBUFFER,se,ne,he,0,de):i.framebufferTexture2D(i.FRAMEBUFFER,se,ne,he,0);else if(E.depthTexture.format===or)At(E)?l.framebufferTexture2DMultisampleEXT(i.FRAMEBUFFER,se,ne,he,0,de):i.framebufferTexture2D(i.FRAMEBUFFER,se,ne,he,0);else throw new Error("THREE.WebGLTextures: Unknown depthTexture format.")}function Ve(N){let E=r.get(N),X=N.isWebGLCubeRenderTarget===!0;if(E.__boundDepthTexture!==N.depthTexture){let K=N.depthTexture;if(E.__depthDisposeCallback&&E.__depthDisposeCallback(),K){let te=()=>{delete E.__boundDepthTexture,delete E.__depthDisposeCallback,K.removeEventListener("dispose",te)};K.addEventListener("dispose",te),E.__depthDisposeCallback=te}E.__boundDepthTexture=K}if(N.depthTexture&&!E.__autoAllocateDepthBuffer)if(X)for(let K=0;K<6;K++)ut(E.__webglFramebuffer[K],N,K);else{let K=N.texture.mipmaps;K&&K.length>0?ut(E.__webglFramebuffer[0],N,0):ut(E.__webglFramebuffer,N,0)}else if(X){E.__webglDepthbuffer=[];for(let K=0;K<6;K++)if(n.bindFramebuffer(i.FRAMEBUFFER,E.__webglFramebuffer[K]),E.__webglDepthbuffer[K]===void 0)E.__webglDepthbuffer[K]=i.createRenderbuffer(),Be(E.__webglDepthbuffer[K],N,!1);else{let te=N.stencilBuffer?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT,he=E.__webglDepthbuffer[K];i.bindRenderbuffer(i.RENDERBUFFER,he),i.framebufferRenderbuffer(i.FRAMEBUFFER,te,i.RENDERBUFFER,he)}}else{let K=N.texture.mipmaps;if(K&&K.length>0?n.bindFramebuffer(i.FRAMEBUFFER,E.__webglFramebuffer[0]):n.bindFramebuffer(i.FRAMEBUFFER,E.__webglFramebuffer),E.__webglDepthbuffer===void 0)E.__webglDepthbuffer=i.createRenderbuffer(),Be(E.__webglDepthbuffer,N,!1);else{let te=N.stencilBuffer?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT,he=E.__webglDepthbuffer;i.bindRenderbuffer(i.RENDERBUFFER,he),i.framebufferRenderbuffer(i.FRAMEBUFFER,te,i.RENDERBUFFER,he)}}n.bindFramebuffer(i.FRAMEBUFFER,null)}function $e(N,E,X){let K=r.get(N);E!==void 0&&ue(K.__webglFramebuffer,N,N.texture,i.COLOR_ATTACHMENT0,i.TEXTURE_2D,0),X!==void 0&&Ve(N)}function Je(N){let E=N.texture,X=r.get(N),K=r.get(E);N.addEventListener("dispose",x);let te=N.textures,he=N.isWebGLCubeRenderTarget===!0,de=te.length>1;if(de||(K.__webglTexture===void 0&&(K.__webglTexture=i.createTexture()),K.__version=E.version,a.memory.textures++),he){X.__webglFramebuffer=[];for(let ne=0;ne<6;ne++)if(E.mipmaps&&E.mipmaps.length>0){X.__webglFramebuffer[ne]=[];for(let se=0;se<E.mipmaps.length;se++)X.__webglFramebuffer[ne][se]=i.createFramebuffer()}else X.__webglFramebuffer[ne]=i.createFramebuffer()}else{if(E.mipmaps&&E.mipmaps.length>0){X.__webglFramebuffer=[];for(let ne=0;ne<E.mipmaps.length;ne++)X.__webglFramebuffer[ne]=i.createFramebuffer()}else X.__webglFramebuffer=i.createFramebuffer();if(de)for(let ne=0,se=te.length;ne<se;ne++){let pe=r.get(te[ne]);pe.__webglTexture===void 0&&(pe.__webglTexture=i.createTexture(),a.memory.textures++)}if(N.samples>0&&At(N)===!1){X.__webglMultisampledFramebuffer=i.createFramebuffer(),X.__webglColorRenderbuffer=[],n.bindFramebuffer(i.FRAMEBUFFER,X.__webglMultisampledFramebuffer);for(let ne=0;ne<te.length;ne++){let se=te[ne];X.__webglColorRenderbuffer[ne]=i.createRenderbuffer(),i.bindRenderbuffer(i.RENDERBUFFER,X.__webglColorRenderbuffer[ne]);let pe=o.convert(se.format,se.colorSpace),De=o.convert(se.type),ve=y(se.internalFormat,pe,De,se.normalized,se.colorSpace,N.isXRRenderTarget===!0),me=St(N);i.renderbufferStorageMultisample(i.RENDERBUFFER,me,ve,N.width,N.height),i.framebufferRenderbuffer(i.FRAMEBUFFER,i.COLOR_ATTACHMENT0+ne,i.RENDERBUFFER,X.__webglColorRenderbuffer[ne])}i.bindRenderbuffer(i.RENDERBUFFER,null),N.depthBuffer&&(X.__webglDepthRenderbuffer=i.createRenderbuffer(),Be(X.__webglDepthRenderbuffer,N,!0)),n.bindFramebuffer(i.FRAMEBUFFER,null)}}if(he){n.bindTexture(i.TEXTURE_CUBE_MAP,K.__webglTexture),ee(i.TEXTURE_CUBE_MAP,E);for(let ne=0;ne<6;ne++)if(E.mipmaps&&E.mipmaps.length>0)for(let se=0;se<E.mipmaps.length;se++)ue(X.__webglFramebuffer[ne][se],N,E,i.COLOR_ATTACHMENT0,i.TEXTURE_CUBE_MAP_POSITIVE_X+ne,se);else ue(X.__webglFramebuffer[ne],N,E,i.COLOR_ATTACHMENT0,i.TEXTURE_CUBE_MAP_POSITIVE_X+ne,0);m(E)&&w(i.TEXTURE_CUBE_MAP),n.unbindTexture()}else if(de){for(let ne=0,se=te.length;ne<se;ne++){let pe=te[ne],De=r.get(pe),ve=i.TEXTURE_2D;(N.isWebGL3DRenderTarget||N.isWebGLArrayRenderTarget)&&(ve=N.isWebGL3DRenderTarget?i.TEXTURE_3D:i.TEXTURE_2D_ARRAY),n.bindTexture(ve,De.__webglTexture),ee(ve,pe),ue(X.__webglFramebuffer,N,pe,i.COLOR_ATTACHMENT0+ne,ve,0),m(pe)&&w(ve)}n.unbindTexture()}else{let ne=i.TEXTURE_2D;if((N.isWebGL3DRenderTarget||N.isWebGLArrayRenderTarget)&&(ne=N.isWebGL3DRenderTarget?i.TEXTURE_3D:i.TEXTURE_2D_ARRAY),n.bindTexture(ne,K.__webglTexture),ee(ne,E),E.mipmaps&&E.mipmaps.length>0)for(let se=0;se<E.mipmaps.length;se++)ue(X.__webglFramebuffer[se],N,E,i.COLOR_ATTACHMENT0,ne,se);else ue(X.__webglFramebuffer,N,E,i.COLOR_ATTACHMENT0,ne,0);m(E)&&w(ne),n.unbindTexture()}N.depthBuffer&&Ve(N)}function Ge(N){let E=N.textures;for(let X=0,K=E.length;X<K;X++){let te=E[X];if(m(te)){let he=T(N),de=r.get(te).__webglTexture;n.bindTexture(he,de),w(he),n.unbindTexture()}}}let tt=[],Dt=[];function fn(N){if(N.samples>0){if(At(N)===!1){let E=N.textures,X=N.width,K=N.height,te=i.COLOR_BUFFER_BIT,he=N.stencilBuffer?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT,de=r.get(N),ne=E.length>1;if(ne)for(let pe=0;pe<E.length;pe++)n.bindFramebuffer(i.FRAMEBUFFER,de.__webglMultisampledFramebuffer),i.framebufferRenderbuffer(i.FRAMEBUFFER,i.COLOR_ATTACHMENT0+pe,i.RENDERBUFFER,null),n.bindFramebuffer(i.FRAMEBUFFER,de.__webglFramebuffer),i.framebufferTexture2D(i.DRAW_FRAMEBUFFER,i.COLOR_ATTACHMENT0+pe,i.TEXTURE_2D,null,0);n.bindFramebuffer(i.READ_FRAMEBUFFER,de.__webglMultisampledFramebuffer);let se=N.texture.mipmaps;se&&se.length>0?n.bindFramebuffer(i.DRAW_FRAMEBUFFER,de.__webglFramebuffer[0]):n.bindFramebuffer(i.DRAW_FRAMEBUFFER,de.__webglFramebuffer);for(let pe=0;pe<E.length;pe++){if(N.resolveDepthBuffer&&(N.depthBuffer&&(te|=i.DEPTH_BUFFER_BIT),N.stencilBuffer&&N.resolveStencilBuffer&&(te|=i.STENCIL_BUFFER_BIT)),ne){i.framebufferRenderbuffer(i.READ_FRAMEBUFFER,i.COLOR_ATTACHMENT0,i.RENDERBUFFER,de.__webglColorRenderbuffer[pe]);let De=r.get(E[pe]).__webglTexture;i.framebufferTexture2D(i.DRAW_FRAMEBUFFER,i.COLOR_ATTACHMENT0,i.TEXTURE_2D,De,0)}i.blitFramebuffer(0,0,X,K,0,0,X,K,te,i.NEAREST),c===!0&&(tt.length=0,Dt.length=0,tt.push(i.COLOR_ATTACHMENT0+pe),N.depthBuffer&&N.storeMultisampledDepthBuffer===!1&&(tt.push(he),Dt.push(he),i.invalidateFramebuffer(i.DRAW_FRAMEBUFFER,Dt)),i.invalidateFramebuffer(i.READ_FRAMEBUFFER,tt))}if(n.bindFramebuffer(i.READ_FRAMEBUFFER,null),n.bindFramebuffer(i.DRAW_FRAMEBUFFER,null),ne)for(let pe=0;pe<E.length;pe++){n.bindFramebuffer(i.FRAMEBUFFER,de.__webglMultisampledFramebuffer),i.framebufferRenderbuffer(i.FRAMEBUFFER,i.COLOR_ATTACHMENT0+pe,i.RENDERBUFFER,de.__webglColorRenderbuffer[pe]);let De=r.get(E[pe]).__webglTexture;n.bindFramebuffer(i.FRAMEBUFFER,de.__webglFramebuffer),i.framebufferTexture2D(i.DRAW_FRAMEBUFFER,i.COLOR_ATTACHMENT0+pe,i.TEXTURE_2D,De,0)}n.bindFramebuffer(i.DRAW_FRAMEBUFFER,de.__webglMultisampledFramebuffer)}else if(N.depthBuffer&&N.storeMultisampledDepthBuffer===!1&&c){let E=N.stencilBuffer?i.DEPTH_STENCIL_ATTACHMENT:i.DEPTH_ATTACHMENT;i.invalidateFramebuffer(i.DRAW_FRAMEBUFFER,[E])}}}function St(N){return Math.min(s.maxSamples,N.samples)}function At(N){let E=r.get(N);return N.samples>0&&e.has("WEBGL_multisampled_render_to_texture")===!0&&E.__useRenderToTexture!==!1}function H(N){let E=a.render.frame;h.get(N)!==E&&(h.set(N,E),N.update())}function Xt(N,E){let X=N.colorSpace,K=N.format,te=N.type;return N.isCompressedTexture===!0||N.isVideoTexture===!0||X!==wo&&X!==Pi&&(Ke.getTransfer(X)===rt?(K!==Nn||te!==mn)&&Ue("WebGLTextures: sRGB encoded textures have to use RGBAFormat and UnsignedByteType."):ze("WebGLTextures: Unsupported texture color space:",X)),E}function at(N){return typeof HTMLImageElement<"u"&&N instanceof HTMLImageElement?(u.width=N.naturalWidth||N.width,u.height=N.naturalHeight||N.height):typeof VideoFrame<"u"&&N instanceof VideoFrame?(u.width=N.displayWidth,u.height=N.displayHeight):(u.width=N.width,u.height=N.height),u}this.allocateTextureUnit=D,this.resetTextureUnits=U,this.getTextureUnits=M,this.setTextureUnits=I,this.setTexture2D=q,this.setTexture2DArray=W,this.setTexture3D=j,this.setTextureCube=B,this.rebindTextures=$e,this.setupRenderTarget=Je,this.updateRenderTargetMipmap=Ge,this.updateMultisampleRenderTarget=fn,this.setupDepthRenderbuffer=Ve,this.setupFrameBufferTexture=ue,this.useMultisampledRTT=At,this.isReversedDepthBuffer=function(){return n.buffers.depth.getReversed()}}function jA(i,e){function n(r,s=Pi){let o,a=Ke.getTransfer(s);if(r===mn)return i.UNSIGNED_BYTE;if(r===gc)return i.UNSIGNED_SHORT_4_4_4_4;if(r===_c)return i.UNSIGNED_SHORT_5_5_5_1;if(r===Df)return i.UNSIGNED_INT_5_9_9_9_REV;if(r===Of)return i.UNSIGNED_INT_10F_11F_11F_REV;if(r===If)return i.BYTE;if(r===Lf)return i.SHORT;if(r===Bs)return i.UNSIGNED_SHORT;if(r===mc)return i.INT;if(r===Kn)return i.UNSIGNED_INT;if(r===Jn)return i.FLOAT;if(r===En)return i.HALF_FLOAT;if(r===Nf)return i.ALPHA;if(r===Uf)return i.RGB;if(r===Nn)return i.RGBA;if(r===fi)return i.DEPTH_COMPONENT;if(r===or)return i.DEPTH_STENCIL;if(r===Ff)return i.RED;if(r===vc)return i.RED_INTEGER;if(r===ar)return i.RG;if(r===yc)return i.RG_INTEGER;if(r===xc)return i.RGBA_INTEGER;if(r===qo||r===jo||r===Yo||r===$o)if(a===rt)if(o=e.get("WEBGL_compressed_texture_s3tc_srgb"),o!==null){if(r===qo)return o.COMPRESSED_SRGB_S3TC_DXT1_EXT;if(r===jo)return o.COMPRESSED_SRGB_ALPHA_S3TC_DXT1_EXT;if(r===Yo)return o.COMPRESSED_SRGB_ALPHA_S3TC_DXT3_EXT;if(r===$o)return o.COMPRESSED_SRGB_ALPHA_S3TC_DXT5_EXT}else return null;else if(o=e.get("WEBGL_compressed_texture_s3tc"),o!==null){if(r===qo)return o.COMPRESSED_RGB_S3TC_DXT1_EXT;if(r===jo)return o.COMPRESSED_RGBA_S3TC_DXT1_EXT;if(r===Yo)return o.COMPRESSED_RGBA_S3TC_DXT3_EXT;if(r===$o)return o.COMPRESSED_RGBA_S3TC_DXT5_EXT}else return null;if(r===bc||r===Sc||r===wc||r===Mc)if(o=e.get("WEBGL_compressed_texture_pvrtc"),o!==null){if(r===bc)return o.COMPRESSED_RGB_PVRTC_4BPPV1_IMG;if(r===Sc)return o.COMPRESSED_RGB_PVRTC_2BPPV1_IMG;if(r===wc)return o.COMPRESSED_RGBA_PVRTC_4BPPV1_IMG;if(r===Mc)return o.COMPRESSED_RGBA_PVRTC_2BPPV1_IMG}else return null;if(r===Ec||r===Ac||r===Tc||r===Cc||r===Rc||r===Zo||r===Pc)if(o=e.get("WEBGL_compressed_texture_etc"),o!==null){if(r===Ec||r===Ac)return a===rt?o.COMPRESSED_SRGB8_ETC2:o.COMPRESSED_RGB8_ETC2;if(r===Tc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ETC2_EAC:o.COMPRESSED_RGBA8_ETC2_EAC;if(r===Cc)return o.COMPRESSED_R11_EAC;if(r===Rc)return o.COMPRESSED_SIGNED_R11_EAC;if(r===Zo)return o.COMPRESSED_RG11_EAC;if(r===Pc)return o.COMPRESSED_SIGNED_RG11_EAC}else return null;if(r===Ic||r===Lc||r===Dc||r===Oc||r===Nc||r===Uc||r===Fc||r===kc||r===Bc||r===zc||r===Vc||r===Gc||r===Hc||r===Wc)if(o=e.get("WEBGL_compressed_texture_astc"),o!==null){if(r===Ic)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_4x4_KHR:o.COMPRESSED_RGBA_ASTC_4x4_KHR;if(r===Lc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_5x4_KHR:o.COMPRESSED_RGBA_ASTC_5x4_KHR;if(r===Dc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_5x5_KHR:o.COMPRESSED_RGBA_ASTC_5x5_KHR;if(r===Oc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_6x5_KHR:o.COMPRESSED_RGBA_ASTC_6x5_KHR;if(r===Nc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_6x6_KHR:o.COMPRESSED_RGBA_ASTC_6x6_KHR;if(r===Uc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_8x5_KHR:o.COMPRESSED_RGBA_ASTC_8x5_KHR;if(r===Fc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_8x6_KHR:o.COMPRESSED_RGBA_ASTC_8x6_KHR;if(r===kc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_8x8_KHR:o.COMPRESSED_RGBA_ASTC_8x8_KHR;if(r===Bc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_10x5_KHR:o.COMPRESSED_RGBA_ASTC_10x5_KHR;if(r===zc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_10x6_KHR:o.COMPRESSED_RGBA_ASTC_10x6_KHR;if(r===Vc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_10x8_KHR:o.COMPRESSED_RGBA_ASTC_10x8_KHR;if(r===Gc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_10x10_KHR:o.COMPRESSED_RGBA_ASTC_10x10_KHR;if(r===Hc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_12x10_KHR:o.COMPRESSED_RGBA_ASTC_12x10_KHR;if(r===Wc)return a===rt?o.COMPRESSED_SRGB8_ALPHA8_ASTC_12x12_KHR:o.COMPRESSED_RGBA_ASTC_12x12_KHR}else return null;if(r===Xc||r===qc||r===jc)if(o=e.get("EXT_texture_compression_bptc"),o!==null){if(r===Xc)return a===rt?o.COMPRESSED_SRGB_ALPHA_BPTC_UNORM_EXT:o.COMPRESSED_RGBA_BPTC_UNORM_EXT;if(r===qc)return o.COMPRESSED_RGB_BPTC_SIGNED_FLOAT_EXT;if(r===jc)return o.COMPRESSED_RGB_BPTC_UNSIGNED_FLOAT_EXT}else return null;if(r===Yc||r===$c||r===Ko||r===Zc)if(o=e.get("EXT_texture_compression_rgtc"),o!==null){if(r===Yc)return o.COMPRESSED_RED_RGTC1_EXT;if(r===$c)return o.COMPRESSED_SIGNED_RED_RGTC1_EXT;if(r===Ko)return o.COMPRESSED_RED_GREEN_RGTC2_EXT;if(r===Zc)return o.COMPRESSED_SIGNED_RED_GREEN_RGTC2_EXT}else return null;return r===zs?i.UNSIGNED_INT_24_8:i[r]!==void 0?i[r]:null}return{convert:n}}var YA=`
void main() {

	gl_Position = vec4( position, 1.0 );

}`,$A=`
uniform sampler2DArray depthColor;
uniform float depthWidth;
uniform float depthHeight;

void main() {

	vec2 coord = vec2( gl_FragCoord.x / depthWidth, gl_FragCoord.y / depthHeight );

	if ( coord.x >= 1.0 ) {

		gl_FragDepth = texture( depthColor, vec3( coord.x - 1.0, coord.y, 1 ) ).r;

	} else {

		gl_FragDepth = texture( depthColor, vec3( coord.x, coord.y, 0 ) ).r;

	}

}`,ud=class{constructor(){this.texture=null,this.mesh=null,this.depthNear=0,this.depthFar=0}init(e,n){if(this.texture===null){let r=new Io(e.texture);(e.depthNear!==n.depthNear||e.depthFar!==n.depthFar)&&(this.depthNear=e.depthNear,this.depthFar=e.depthFar),this.texture=r}}getMesh(e){if(this.texture!==null&&this.mesh===null){let n=e.cameras[0].viewport,r=new Jt({vertexShader:YA,fragmentShader:$A,uniforms:{depthColor:{value:this.texture},depthWidth:{value:n.z},depthHeight:{value:n.w}}});this.mesh=new kt(new Oo(20,20),r)}return this.mesh}reset(){this.texture=null,this.mesh=null}getDepthTexture(){return this.texture}},hd=class extends Yn{constructor(e,n){super();let r=this,s=null,o=1,a=null,l="local-floor",c=1,u=null,h=null,d=null,f=null,p=null,g=null,v=typeof XRWebGLBinding<"u",_=new ud,m={},w=n.getContextAttributes(),T=null,y=null,b=[],S=[],A=new fe,x=null,C=null,L=new $t;L.viewport=new vt;let P=new $t;P.viewport=new vt;let O=[L,P],U=new cc,M=null,I=null;this.cameraAutoUpdate=!0,this.enabled=!1,this.isPresenting=!1,this.getController=function(V){let Z=b[V];return Z===void 0&&(Z=new Rs,b[V]=Z),Z.getTargetRaySpace()},this.getControllerGrip=function(V){let Z=b[V];return Z===void 0&&(Z=new Rs,b[V]=Z),Z.getGripSpace()},this.getHand=function(V){let Z=b[V];return Z===void 0&&(Z=new Rs,b[V]=Z),Z.getHandSpace()};function D(V){let Z=S.indexOf(V.inputSource);if(Z===-1)return;let ce=b[Z];ce!==void 0&&(ce.update(V.inputSource,V.frame,u||a),ce.dispatchEvent({type:V.type,data:V.inputSource}))}function k(){s.removeEventListener("select",D),s.removeEventListener("selectstart",D),s.removeEventListener("selectend",D),s.removeEventListener("squeeze",D),s.removeEventListener("squeezestart",D),s.removeEventListener("squeezeend",D),s.removeEventListener("end",k),s.removeEventListener("inputsourceschange",q);for(let V=0;V<b.length;V++){let Z=S[V];Z!==null&&(S[V]=null,b[V].disconnect(Z))}M=null,I=null,_.reset();for(let V in m)delete m[V];if(e.setRenderTarget(T),p=null,f=null,d=null,s=null,y=null,oe.stop(),r.isPresenting=!1,e.setPixelRatio(x),e.setSize(A.width,A.height,!1),C!==null){let V=C.camera;V.fov=C.fov,V.zoom=C.zoom,V.updateProjectionMatrix(),C=null}r.dispatchEvent({type:"sessionend"})}this.setFramebufferScaleFactor=function(V){o=V,r.isPresenting===!0&&Ue("WebXRManager: Cannot change framebuffer scale while presenting.")},this.setReferenceSpaceType=function(V){l=V,r.isPresenting===!0&&Ue("WebXRManager: Cannot change reference space type while presenting.")},this.getReferenceSpace=function(){return u||a},this.setReferenceSpace=function(V){u=V},this.getBaseLayer=function(){return f!==null?f:p},this.getBinding=function(){return d===null&&v&&(d=new XRWebGLBinding(s,n)),d},this.getFrame=function(){return g},this.getSession=function(){return s},this.setSession=async function(V){if(s=V,s!==null){if(T=e.getRenderTarget(),s.addEventListener("select",D),s.addEventListener("selectstart",D),s.addEventListener("selectend",D),s.addEventListener("squeeze",D),s.addEventListener("squeezestart",D),s.addEventListener("squeezeend",D),s.addEventListener("end",k),s.addEventListener("inputsourceschange",q),w.xrCompatible!==!0&&await n.makeXRCompatible(),x=e.getPixelRatio(),e.getSize(A),v&&"createProjectionLayer"in XRWebGLBinding.prototype){let ce=null,Ee=null,ue=null;w.depth&&(ue=w.stencil?n.DEPTH24_STENCIL8:n.DEPTH_COMPONENT24,ce=w.stencil?or:fi,Ee=w.stencil?zs:Kn);let Be={colorFormat:n.RGBA8,depthFormat:ue,scaleFactor:o};d=this.getBinding(),f=d.createProjectionLayer(Be),s.updateRenderState({layers:[f]}),e.setPixelRatio(1),e.setSize(f.textureWidth,f.textureHeight,!1),y=new Zt(f.textureWidth,f.textureHeight,{format:Nn,type:mn,depthTexture:new Ji(f.textureWidth,f.textureHeight,Ee,void 0,void 0,void 0,void 0,void 0,void 0,ce),stencilBuffer:w.stencil,colorSpace:e.outputColorSpace,samples:w.antialias?4:0,resolveDepthBuffer:f.ignoreDepthValues===!1,resolveStencilBuffer:f.ignoreDepthValues===!1,storeMultisampledDepthBuffer:f.ignoreDepthValues===!1,storeMultisampledStencilBuffer:f.ignoreDepthValues===!1})}else{let ce={antialias:w.antialias,alpha:!0,depth:w.depth,stencil:w.stencil,framebufferScaleFactor:o};p=new XRWebGLLayer(s,n,ce),s.updateRenderState({baseLayer:p}),e.setPixelRatio(1),e.setSize(p.framebufferWidth,p.framebufferHeight,!1),y=new Zt(p.framebufferWidth,p.framebufferHeight,{format:Nn,type:mn,colorSpace:e.outputColorSpace,stencilBuffer:w.stencil,resolveDepthBuffer:p.ignoreDepthValues===!1,resolveStencilBuffer:p.ignoreDepthValues===!1,storeMultisampledDepthBuffer:p.ignoreDepthValues===!1,storeMultisampledStencilBuffer:p.ignoreDepthValues===!1})}y.isXRRenderTarget=!0,this.setFoveation(c),u=null,a=await s.requestReferenceSpace(l),oe.setContext(s),oe.start(),r.isPresenting=!0,r.dispatchEvent({type:"sessionstart"})}},this.getEnvironmentBlendMode=function(){if(s!==null)return s.environmentBlendMode},this.getDepthTexture=function(){return _.getDepthTexture()};function q(V){for(let Z=0;Z<V.removed.length;Z++){let ce=V.removed[Z],Ee=S.indexOf(ce);Ee>=0&&(S[Ee]=null,b[Ee].disconnect(ce))}for(let Z=0;Z<V.added.length;Z++){let ce=V.added[Z],Ee=S.indexOf(ce);if(Ee===-1){for(let Be=0;Be<b.length;Be++)if(Be>=S.length){S.push(ce),Ee=Be;break}else if(S[Be]===null){S[Be]=ce,Ee=Be;break}if(Ee===-1)break}let ue=b[Ee];ue&&ue.connect(ce)}}let W=new F,j=new F;function B(V,Z,ce){W.setFromMatrixPosition(Z.matrixWorld),j.setFromMatrixPosition(ce.matrixWorld);let Ee=W.distanceTo(j),ue=Z.projectionMatrix.elements,Be=ce.projectionMatrix.elements,ut=ue[14]/(ue[10]-1),Ve=ue[14]/(ue[10]+1),$e=(ue[9]+1)/ue[5],Je=(ue[9]-1)/ue[5],Ge=(ue[8]-1)/ue[0],tt=(Be[8]+1)/Be[0],Dt=ut*Ge,fn=ut*tt,St=Ee/(-Ge+tt),At=St*-Ge;if(Z.matrixWorld.decompose(V.position,V.quaternion,V.scale),V.translateX(At),V.translateZ(St),V.matrixWorld.compose(V.position,V.quaternion,V.scale),V.matrixWorldInverse.copy(V.matrixWorld).invert(),ue[10]===-1)V.projectionMatrix.copy(Z.projectionMatrix),V.projectionMatrixInverse.copy(Z.projectionMatrixInverse);else{let H=ut+St,Xt=Ve+St,at=Dt-At,N=fn+(Ee-At),E=$e*Ve/Xt*H,X=Je*Ve/Xt*H;V.projectionMatrix.makePerspective(at,N,E,X,H,Xt),V.projectionMatrixInverse.copy(V.projectionMatrix).invert()}}function Q(V,Z){Z===null?V.matrixWorld.copy(V.matrix):V.matrixWorld.multiplyMatrices(Z.matrixWorld,V.matrix),V.matrixWorldInverse.copy(V.matrixWorld).invert()}this.updateCamera=function(V){if(s===null)return;let Z=V.near,ce=V.far;_.texture!==null&&(_.depthNear>0&&(Z=_.depthNear),_.depthFar>0&&(ce=_.depthFar)),U.near=P.near=L.near=Z,U.far=P.far=L.far=ce,(M!==U.near||I!==U.far)&&(s.updateRenderState({depthNear:U.near,depthFar:U.far}),M=U.near,I=U.far),U.layers.mask=V.layers.mask|6,L.layers.mask=U.layers.mask&-5,P.layers.mask=U.layers.mask&-3;let Ee=V.parent,ue=U.cameras;Q(U,Ee);for(let Be=0;Be<ue.length;Be++)Q(ue[Be],Ee);ue.length===2?B(U,L,P):U.projectionMatrix.copy(L.projectionMatrix),C===null&&V.isPerspectiveCamera&&(C={camera:V,fov:V.fov,zoom:V.zoom}),re(V,U,Ee)};function re(V,Z,ce){ce===null?V.matrix.copy(Z.matrixWorld):(V.matrix.copy(ce.matrixWorld),V.matrix.invert(),V.matrix.multiply(Z.matrixWorld)),V.matrix.decompose(V.position,V.quaternion,V.scale),V.updateMatrixWorld(!0),V.projectionMatrix.copy(Z.projectionMatrix),V.projectionMatrixInverse.copy(Z.projectionMatrixInverse),V.isPerspectiveCamera&&(V.fov=As*2*Math.atan(1/V.projectionMatrix.elements[5]),V.zoom=1)}this.getCamera=function(){return U},this.getFoveation=function(){if(!(f===null&&p===null))return c},this.setFoveation=function(V){c=V,f!==null&&(f.fixedFoveation=V),p!==null&&p.fixedFoveation!==void 0&&(p.fixedFoveation=V)},this.hasDepthSensing=function(){return _.texture!==null},this.getDepthSensingMesh=function(){return _.getMesh(U)},this.getCameraTexture=function(V){return m[V]};let be=null;function ee(V,Z){if(h=Z.getViewerPose(u||a),g=Z,h!==null){let ce=h.views;p!==null&&(e.setRenderTargetFramebuffer(y,p.framebuffer),e.setRenderTarget(y));let Ee=!1;ce.length!==U.cameras.length&&(U.cameras.length=0,Ee=!0);for(let Ve=0;Ve<ce.length;Ve++){let $e=ce[Ve],Je=null;if(p!==null)Je=p.getViewport($e);else{let tt=d.getViewSubImage(f,$e);Je=tt.viewport,Ve===0&&(e.setRenderTargetTextures(y,tt.colorTexture,tt.depthStencilTexture),e.setRenderTarget(y))}let Ge=O[Ve];Ge===void 0&&(Ge=new $t,Ge.layers.enable(Ve),Ge.viewport=new vt,O[Ve]=Ge),Ge.matrix.fromArray($e.transform.matrix),Ge.matrix.decompose(Ge.position,Ge.quaternion,Ge.scale),Ge.projectionMatrix.fromArray($e.projectionMatrix),Ge.projectionMatrixInverse.copy(Ge.projectionMatrix).invert(),Ge.viewport.set(Je.x,Je.y,Je.width,Je.height),Ve===0&&(U.matrix.copy(Ge.matrix),U.matrix.decompose(U.position,U.quaternion,U.scale)),Ee===!0&&U.cameras.push(Ge)}let ue=s.enabledFeatures;if(ue&&ue.includes("depth-sensing")&&s.depthUsage=="gpu-optimized"&&v){d=r.getBinding();let Ve=d.getDepthInformation(ce[0]);Ve&&Ve.isValid&&Ve.texture&&_.init(Ve,s.renderState)}if(ue&&ue.includes("camera-access")&&v){e.state.unbindTexture(),d=r.getBinding();for(let Ve=0;Ve<ce.length;Ve++){let $e=ce[Ve].camera;if($e){let Je=m[$e];Je||(Je=new Io,m[$e]=Je);let Ge=d.getCameraImage($e);Je.sourceTexture=Ge}}}}for(let ce=0;ce<b.length;ce++){let Ee=S[ce],ue=b[ce];Ee!==null&&ue!==void 0&&ue.update(Ee,Z,u||a)}be&&be(V,Z),Z.detectedPlanes&&r.dispatchEvent({type:"planesdetected",data:Z}),g=null}let oe=new Zg;oe.setAnimationLoop(ee),this.setAnimationLoop=function(V){be=V},this.dispose=function(){}}},ZA=new st,n0=new He;n0.set(-1,0,0,0,1,0,0,0,1);function KA(i,e){function n(_,m){_.matrixAutoUpdate===!0&&_.updateMatrix(),m.value.copy(_.matrix)}function r(_,m){m.color.getRGB(_.fogColor.value,Gf(i)),m.isFog?(_.fogNear.value=m.near,_.fogFar.value=m.far):m.isFogExp2&&(_.fogDensity.value=m.density)}function s(_,m,w,T,y){m.isNodeMaterial?m.uniformsNeedUpdate=!1:m.isMeshBasicMaterial?o(_,m):m.isMeshLambertMaterial?(o(_,m),m.envMap&&(_.envMapIntensity.value=m.envMapIntensity)):m.isMeshToonMaterial?(o(_,m),d(_,m)):m.isMeshPhongMaterial?(o(_,m),h(_,m),m.envMap&&(_.envMapIntensity.value=m.envMapIntensity)):m.isMeshStandardMaterial?(o(_,m),f(_,m),m.isMeshPhysicalMaterial&&p(_,m,y)):m.isMeshMatcapMaterial?(o(_,m),g(_,m)):m.isMeshDepthMaterial?o(_,m):m.isMeshDistanceMaterial?(o(_,m),v(_,m)):m.isMeshNormalMaterial?o(_,m):m.isLineBasicMaterial?(a(_,m),m.isLineDashedMaterial&&l(_,m)):m.isPointsMaterial?c(_,m,w,T):m.isSpriteMaterial?u(_,m):m.isShadowMaterial?(_.color.value.copy(m.color),_.opacity.value=m.opacity):m.isShaderMaterial&&(m.uniformsNeedUpdate=!1)}function o(_,m){_.opacity.value=m.opacity,m.color&&_.diffuse.value.copy(m.color),m.emissive&&_.emissive.value.copy(m.emissive).multiplyScalar(m.emissiveIntensity),m.map&&(_.map.value=m.map,n(m.map,_.mapTransform)),m.alphaMap&&(_.alphaMap.value=m.alphaMap,n(m.alphaMap,_.alphaMapTransform)),m.bumpMap&&(_.bumpMap.value=m.bumpMap,n(m.bumpMap,_.bumpMapTransform),_.bumpScale.value=m.bumpScale,m.side===Gt&&(_.bumpScale.value*=-1)),m.normalMap&&(_.normalMap.value=m.normalMap,n(m.normalMap,_.normalMapTransform),_.normalScale.value.copy(m.normalScale),m.side===Gt&&_.normalScale.value.negate()),m.displacementMap&&(_.displacementMap.value=m.displacementMap,n(m.displacementMap,_.displacementMapTransform),_.displacementScale.value=m.displacementScale,_.displacementBias.value=m.displacementBias),m.emissiveMap&&(_.emissiveMap.value=m.emissiveMap,n(m.emissiveMap,_.emissiveMapTransform)),m.specularMap&&(_.specularMap.value=m.specularMap,n(m.specularMap,_.specularMapTransform)),m.alphaTest>0&&(_.alphaTest.value=m.alphaTest);let w=e.get(m),T=w.envMap,y=w.envMapRotation;T&&(_.envMap.value=T,_.envMapRotation.value.setFromMatrix4(ZA.makeRotationFromEuler(y)).transpose(),T.isCubeTexture&&T.isRenderTargetTexture===!1&&_.envMapRotation.value.premultiply(n0),_.reflectivity.value=m.reflectivity,_.ior.value=m.ior,_.refractionRatio.value=m.refractionRatio),m.lightMap&&(_.lightMap.value=m.lightMap,_.lightMapIntensity.value=m.lightMapIntensity,n(m.lightMap,_.lightMapTransform)),m.aoMap&&(_.aoMap.value=m.aoMap,_.aoMapIntensity.value=m.aoMapIntensity,n(m.aoMap,_.aoMapTransform))}function a(_,m){_.diffuse.value.copy(m.color),_.opacity.value=m.opacity,m.map&&(_.map.value=m.map,n(m.map,_.mapTransform))}function l(_,m){_.dashSize.value=m.dashSize,_.totalSize.value=m.dashSize+m.gapSize,_.scale.value=m.scale}function c(_,m,w,T){_.diffuse.value.copy(m.color),_.opacity.value=m.opacity,_.size.value=m.size*w,_.scale.value=T*.5,m.map&&(_.map.value=m.map,n(m.map,_.uvTransform)),m.alphaMap&&(_.alphaMap.value=m.alphaMap,n(m.alphaMap,_.alphaMapTransform)),m.alphaTest>0&&(_.alphaTest.value=m.alphaTest)}function u(_,m){_.diffuse.value.copy(m.color),_.opacity.value=m.opacity,_.rotation.value=m.rotation,m.map&&(_.map.value=m.map,n(m.map,_.mapTransform)),m.alphaMap&&(_.alphaMap.value=m.alphaMap,n(m.alphaMap,_.alphaMapTransform)),m.alphaTest>0&&(_.alphaTest.value=m.alphaTest)}function h(_,m){_.specular.value.copy(m.specular),_.shininess.value=Math.max(m.shininess,1e-4)}function d(_,m){m.gradientMap&&(_.gradientMap.value=m.gradientMap)}function f(_,m){_.metalness.value=m.metalness,m.metalnessMap&&(_.metalnessMap.value=m.metalnessMap,n(m.metalnessMap,_.metalnessMapTransform)),_.roughness.value=m.roughness,m.roughnessMap&&(_.roughnessMap.value=m.roughnessMap,n(m.roughnessMap,_.roughnessMapTransform)),m.envMap&&(_.envMapIntensity.value=m.envMapIntensity)}function p(_,m,w){_.ior.value=m.ior,m.sheen>0&&(_.sheenColor.value.copy(m.sheenColor).multiplyScalar(m.sheen),_.sheenRoughness.value=m.sheenRoughness,m.sheenColorMap&&(_.sheenColorMap.value=m.sheenColorMap,n(m.sheenColorMap,_.sheenColorMapTransform)),m.sheenRoughnessMap&&(_.sheenRoughnessMap.value=m.sheenRoughnessMap,n(m.sheenRoughnessMap,_.sheenRoughnessMapTransform))),m.clearcoat>0&&(_.clearcoat.value=m.clearcoat,_.clearcoatRoughness.value=m.clearcoatRoughness,m.clearcoatMap&&(_.clearcoatMap.value=m.clearcoatMap,n(m.clearcoatMap,_.clearcoatMapTransform)),m.clearcoatRoughnessMap&&(_.clearcoatRoughnessMap.value=m.clearcoatRoughnessMap,n(m.clearcoatRoughnessMap,_.clearcoatRoughnessMapTransform)),m.clearcoatNormalMap&&(_.clearcoatNormalMap.value=m.clearcoatNormalMap,n(m.clearcoatNormalMap,_.clearcoatNormalMapTransform),_.clearcoatNormalScale.value.copy(m.clearcoatNormalScale),m.side===Gt&&_.clearcoatNormalScale.value.negate())),m.dispersion>0&&(_.dispersion.value=m.dispersion),m.retroreflectivity>0&&(_.retroreflectivity.value=m.retroreflectivity),m.iridescence>0&&(_.iridescence.value=m.iridescence,_.iridescenceIOR.value=m.iridescenceIOR,_.iridescenceThicknessMinimum.value=m.iridescenceThicknessRange[0],_.iridescenceThicknessMaximum.value=m.iridescenceThicknessRange[1],m.iridescenceMap&&(_.iridescenceMap.value=m.iridescenceMap,n(m.iridescenceMap,_.iridescenceMapTransform)),m.iridescenceThicknessMap&&(_.iridescenceThicknessMap.value=m.iridescenceThicknessMap,n(m.iridescenceThicknessMap,_.iridescenceThicknessMapTransform))),m.transmission>0&&(_.transmission.value=m.transmission,_.transmissionSamplerMap.value=w.texture,_.transmissionSamplerSize.value.set(w.width,w.height),m.transmissionMap&&(_.transmissionMap.value=m.transmissionMap,n(m.transmissionMap,_.transmissionMapTransform)),_.thickness.value=m.thickness,m.thicknessMap&&(_.thicknessMap.value=m.thicknessMap,n(m.thicknessMap,_.thicknessMapTransform)),_.attenuationDistance.value=m.attenuationDistance,_.attenuationColor.value.copy(m.attenuationColor)),m.anisotropy>0&&(_.anisotropyVector.value.set(m.anisotropy*Math.cos(m.anisotropyRotation),m.anisotropy*Math.sin(m.anisotropyRotation)),m.anisotropyMap&&(_.anisotropyMap.value=m.anisotropyMap,n(m.anisotropyMap,_.anisotropyMapTransform))),_.specularIntensity.value=m.specularIntensity,_.specularColor.value.copy(m.specularColor),m.specularColorMap&&(_.specularColorMap.value=m.specularColorMap,n(m.specularColorMap,_.specularColorMapTransform)),m.specularIntensityMap&&(_.specularIntensityMap.value=m.specularIntensityMap,n(m.specularIntensityMap,_.specularIntensityMapTransform))}function g(_,m){m.matcap&&(_.matcap.value=m.matcap)}function v(_,m){let w=e.get(m).light;_.referencePosition.value.setFromMatrixPosition(w.matrixWorld),_.nearDistance.value=w.shadow.camera.near,_.farDistance.value=w.shadow.camera.far}return{refreshFogUniforms:r,refreshMaterialUniforms:s}}function JA(i,e,n,r){let s={},o={},a=[],l=i.getParameter(i.MAX_UNIFORM_BUFFER_BINDINGS);function c(y,b){let S=b.program;r.uniformBlockBinding(y,S)}function u(y,b){let S=s[y.id];S===void 0&&(_(y),S=h(y),s[y.id]=S,y.addEventListener("dispose",w));let A=b.program;r.updateUBOMapping(y,A);let x=e.render.frame;o[y.id]!==x&&(f(y),o[y.id]=x)}function h(y){let b=d();y.__bindingPointIndex=b;let S=i.createBuffer(),A=y.__size,x=y.usage;return i.bindBuffer(i.UNIFORM_BUFFER,S),i.bufferData(i.UNIFORM_BUFFER,A,x),i.bindBuffer(i.UNIFORM_BUFFER,null),i.bindBufferBase(i.UNIFORM_BUFFER,b,S),S}function d(){for(let y=0;y<l;y++)if(a.indexOf(y)===-1)return a.push(y),y;return ze("WebGLRenderer: Maximum number of simultaneously usable uniforms groups reached."),0}function f(y){let b=s[y.id],S=y.uniforms,A=y.__cache;i.bindBuffer(i.UNIFORM_BUFFER,b);for(let x=0,C=S.length;x<C;x++){let L=S[x];if(Array.isArray(L))for(let P=0,O=L.length;P<O;P++)p(L[P],x,P,A);else p(L,x,0,A)}i.bindBuffer(i.UNIFORM_BUFFER,null)}function p(y,b,S,A){if(v(y,b,S,A)===!0){let x=y.__offset,C=y.value;if(Array.isArray(C)){let L=0;for(let P=0;P<C.length;P++){let O=C[P],U=m(O);g(O,y.__data,L),typeof O!="number"&&typeof O!="boolean"&&!O.isMatrix3&&!ArrayBuffer.isView(O)&&(L+=U.storage/Float32Array.BYTES_PER_ELEMENT)}}else g(C,y.__data,0);i.bufferSubData(i.UNIFORM_BUFFER,x,y.__data)}}function g(y,b,S){typeof y=="number"||typeof y=="boolean"?b[0]=y:y.isMatrix3?(b[0]=y.elements[0],b[1]=y.elements[1],b[2]=y.elements[2],b[3]=0,b[4]=y.elements[3],b[5]=y.elements[4],b[6]=y.elements[5],b[7]=0,b[8]=y.elements[6],b[9]=y.elements[7],b[10]=y.elements[8],b[11]=0):ArrayBuffer.isView(y)?b.set(new y.constructor(y.buffer,y.byteOffset,b.length)):y.toArray(b,S)}function v(y,b,S,A){let x=y.value,C=b+"_"+S;if(A[C]===void 0)return typeof x=="number"||typeof x=="boolean"?A[C]=x:ArrayBuffer.isView(x)?A[C]=x.slice():A[C]=x.clone(),!0;{let L=A[C];if(typeof x=="number"||typeof x=="boolean"){if(L!==x)return A[C]=x,!0}else{if(ArrayBuffer.isView(x))return!0;if(L.equals(x)===!1)return L.copy(x),!0}}return!1}function _(y){let b=y.uniforms,S=0,A=16;for(let C=0,L=b.length;C<L;C++){let P=Array.isArray(b[C])?b[C]:[b[C]];for(let O=0,U=P.length;O<U;O++){let M=P[O],I=Array.isArray(M.value)?M.value:[M.value];for(let D=0,k=I.length;D<k;D++){let q=I[D],W=m(q),j=S%A,B=j%W.boundary,Q=j+B;S+=B,Q!==0&&A-Q<W.storage&&(S+=A-Q),M.__data=new Float32Array(W.storage/Float32Array.BYTES_PER_ELEMENT),M.__offset=S,S+=W.storage}}}let x=S%A;return x>0&&(S+=A-x),y.__size=S,y.__cache={},this}function m(y){let b={boundary:0,storage:0};return typeof y=="number"||typeof y=="boolean"?(b.boundary=4,b.storage=4):y.isVector2?(b.boundary=8,b.storage=8):y.isVector3||y.isColor?(b.boundary=16,b.storage=12):y.isVector4?(b.boundary=16,b.storage=16):y.isMatrix3?(b.boundary=48,b.storage=48):y.isMatrix4?(b.boundary=64,b.storage=64):y.isTexture?Ue("WebGLRenderer: Texture samplers can not be part of an uniforms group."):ArrayBuffer.isView(y)?(b.boundary=16,b.storage=y.byteLength):Ue("WebGLRenderer: Unsupported uniform value type.",y),b}function w(y){let b=y.target;b.removeEventListener("dispose",w);let S=a.indexOf(b.__bindingPointIndex);a.splice(S,1),i.deleteBuffer(s[b.id]),delete s[b.id],delete o[b.id]}function T(){for(let y in s)i.deleteBuffer(s[y]);a=[],s={},o={}}return{bind:c,update:u,dispose:T}}var QA=new Uint16Array([12469,15057,12620,14925,13266,14620,13807,14376,14323,13990,14545,13625,14713,13328,14840,12882,14931,12528,14996,12233,15039,11829,15066,11525,15080,11295,15085,10976,15082,10705,15073,10495,13880,14564,13898,14542,13977,14430,14158,14124,14393,13732,14556,13410,14702,12996,14814,12596,14891,12291,14937,11834,14957,11489,14958,11194,14943,10803,14921,10506,14893,10278,14858,9960,14484,14039,14487,14025,14499,13941,14524,13740,14574,13468,14654,13106,14743,12678,14818,12344,14867,11893,14889,11509,14893,11180,14881,10751,14852,10428,14812,10128,14765,9754,14712,9466,14764,13480,14764,13475,14766,13440,14766,13347,14769,13070,14786,12713,14816,12387,14844,11957,14860,11549,14868,11215,14855,10751,14825,10403,14782,10044,14729,9651,14666,9352,14599,9029,14967,12835,14966,12831,14963,12804,14954,12723,14936,12564,14917,12347,14900,11958,14886,11569,14878,11247,14859,10765,14828,10401,14784,10011,14727,9600,14660,9289,14586,8893,14508,8533,15111,12234,15110,12234,15104,12216,15092,12156,15067,12010,15028,11776,14981,11500,14942,11205,14902,10752,14861,10393,14812,9991,14752,9570,14682,9252,14603,8808,14519,8445,14431,8145,15209,11449,15208,11451,15202,11451,15190,11438,15163,11384,15117,11274,15055,10979,14994,10648,14932,10343,14871,9936,14803,9532,14729,9218,14645,8742,14556,8381,14461,8020,14365,7603,15273,10603,15272,10607,15267,10619,15256,10631,15231,10614,15182,10535,15118,10389,15042,10167,14963,9787,14883,9447,14800,9115,14710,8665,14615,8318,14514,7911,14411,7507,14279,7198,15314,9675,15313,9683,15309,9712,15298,9759,15277,9797,15229,9773,15166,9668,15084,9487,14995,9274,14898,8910,14800,8539,14697,8234,14590,7790,14479,7409,14367,7067,14178,6621,15337,8619,15337,8631,15333,8677,15325,8769,15305,8871,15264,8940,15202,8909,15119,8775,15022,8565,14916,8328,14804,8009,14688,7614,14569,7287,14448,6888,14321,6483,14088,6171,15350,7402,15350,7419,15347,7480,15340,7613,15322,7804,15287,7973,15229,8057,15148,8012,15046,7846,14933,7611,14810,7357,14682,7069,14552,6656,14421,6316,14251,5948,14007,5528,15356,5942,15356,5977,15353,6119,15348,6294,15332,6551,15302,6824,15249,7044,15171,7122,15070,7050,14949,6861,14818,6611,14679,6349,14538,6067,14398,5651,14189,5311,13935,4958,15359,4123,15359,4153,15356,4296,15353,4646,15338,5160,15311,5508,15263,5829,15188,6042,15088,6094,14966,6001,14826,5796,14678,5543,14527,5287,14377,4985,14133,4586,13869,4257,15360,1563,15360,1642,15358,2076,15354,2636,15341,3350,15317,4019,15273,4429,15203,4732,15105,4911,14981,4932,14836,4818,14679,4621,14517,4386,14359,4156,14083,3795,13808,3437,15360,122,15360,137,15358,285,15355,636,15344,1274,15322,2177,15281,2765,15215,3223,15120,3451,14995,3569,14846,3567,14681,3466,14511,3305,14344,3121,14037,2800,13753,2467,15360,0,15360,1,15359,21,15355,89,15346,253,15325,479,15287,796,15225,1148,15133,1492,15008,1749,14856,1882,14685,1886,14506,1783,14324,1608,13996,1398,13702,1183]),pi=null;function eT(){return pi===null&&(pi=new Fl(QA,16,16,ar,En),pi.name="DFG_LUT",pi.minFilter=Vt,pi.magFilter=Vt,pi.wrapS=ui,pi.wrapT=ui,pi.generateMipmaps=!1,pi.needsUpdate=!0),pi}var su=class{constructor(e={}){let{canvas:n=Sg(),context:r=null,depth:s=!0,stencil:o=!1,alpha:a=!1,antialias:l=!1,premultipliedAlpha:c=!0,preserveDrawingBuffer:u=!1,powerPreference:h="default",failIfMajorPerformanceCaveat:d=!1,reversedDepthBuffer:f=!1,outputBufferType:p=mn}=e;this.isWebGLRenderer=!0;let g;if(r!==null){if(typeof WebGLRenderingContext<"u"&&r instanceof WebGLRenderingContext)throw new Error("THREE.WebGLRenderer: WebGL 1 is not supported since r163.");g=r.getContextAttributes().alpha}else g=a;let v=p,_=new Set([xc,yc,vc]),m=new Set([mn,Kn,Bs,zs,gc,_c]),w=new Uint32Array(4),T=new Int32Array(4),y=new F,b=null,S=null,A=[],x=[],C=null;this.domElement=n,this.debug={checkShaderErrors:!0,diagnostics:{keywords:!1},onShaderError:null},this.autoClear=!0,this.autoClearColor=!0,this.autoClearDepth=!0,this.autoClearStencil=!0,this.sortObjects=!0,this.clippingPlanes=[],this.localClippingEnabled=!1,this.toneMapping=Zn,this.toneMappingExposure=1,this.transmissionResolutionScale=1;let L=this,P=!1,O=null,U=null,M=null,I=null;this._outputColorSpace=on;let D=0,k=0,q=null,W=-1,j=null,B=new vt,Q=new vt,re=null,be=new We(0),ee=0,oe=n.width,V=n.height,Z=1,ce=null,Ee=null,ue=new vt(0,0,oe,V),Be=new vt(0,0,oe,V),ut=!1,Ve=new Ps,$e=!1,Je=!1,Ge=new st,tt=new F,Dt=new vt,fn={background:null,fog:null,environment:null,overrideMaterial:null,isScene:!0},St=!1;function At(){return q===null?Z:1}let H=r;function Xt(R,z){return n.getContext(R,z)}let at,N,E,X,K,te,he,de,ne,se,pe,De,ve,me,Oe,Fe,Xe,G,ge,ie,_e,Me,ae;try{let R={alpha:!0,depth:s,stencil:o,antialias:l,premultipliedAlpha:c,preserveDrawingBuffer:u,powerPreference:h,failIfMajorPerformanceCaveat:d};if("setAttribute"in n&&n.setAttribute("data-engine",`three.js r${"186"}`),n.addEventListener("webglcontextlost",ft,!1),n.addEventListener("webglcontextrestored",nt,!1),n.addEventListener("webglcontextcreationerror",Hn,!1),H===null){let z="webgl2";if(H=Xt(z,R),H===null)throw Xt(z)?new Error("THREE.WebGLRenderer: Error creating WebGL context with your selected attributes."):new Error("THREE.WebGLRenderer: Error creating WebGL context.")}Ne()}catch(R){throw n.removeEventListener("webglcontextlost",ft,!1),n.removeEventListener("webglcontextrestored",nt,!1),n.removeEventListener("webglcontextcreationerror",Hn,!1),ze("WebGLRenderer: "+R.message),R}function Ne(){at=new aE(H),at.init(),_e=new jA(H,at),N=new K1(H,at,e,_e),E=new XA(H,at),N.reversedDepthBuffer&&f&&E.buffers.depth.setReversed(!0),U=H.createFramebuffer(),M=H.createFramebuffer(),I=H.createFramebuffer(),X=new uE(H),K=new IA,te=new qA(H,at,E,K,N,_e,X),he=new oE(L),de=new fw(H),Me=new $1(H,de),ne=new lE(H,de,X,Me),se=new fE(H,ne,de,Me,X),G=new hE(H,N,te),Oe=new J1(K),pe=new PA(L,he,at,N,Me,Oe),De=new KA(L,K),ve=new DA,me=new BA(at),Xe=new Y1(L,he,E,se,g,c),Fe=new WA(L,se,N),ae=new JA(H,X,N,E),ge=new Z1(H,at,X),ie=new cE(H,at,X),X.programs=pe.programs,L.capabilities=N,L.extensions=at,L.properties=K,L.renderLists=ve,L.shadowMap=Fe,L.state=E,L.info=X}v!==mn&&(C=new pE(v,n.width,n.height,l,s,o));let Ie=new hd(L,H);this.xr=Ie,this.getContext=function(){return H},this.getContextAttributes=function(){return H.getContextAttributes()},this.forceContextLoss=function(){let R=at.get("WEBGL_lose_context");R&&R.loseContext()},this.forceContextRestore=function(){let R=at.get("WEBGL_lose_context");R&&R.restoreContext()},this.getPixelRatio=function(){return Z},this.setPixelRatio=function(R){R!==void 0&&(Z=R,this.setSize(oe,V,!1))},this.getSize=function(R){return R.set(oe,V)},this.setSize=function(R,z,J=!0){if(Ie.isPresenting){Ue("WebGLRenderer: Can't change size while VR device is presenting.");return}oe=R,V=z,n.width=Math.floor(R*Z),n.height=Math.floor(z*Z),J===!0&&(n.style.width=R+"px",n.style.height=z+"px"),C!==null&&C.setSize(n.width,n.height),this.setViewport(0,0,R,z)},this.getDrawingBufferSize=function(R){return R.set(oe*Z,V*Z).floor()},this.setDrawingBufferSize=function(R,z,J){oe=R,V=z,Z=J,n.width=Math.floor(R*J),n.height=Math.floor(z*J),this.setViewport(0,0,R,z)},this.setEffects=function(R){if(v===mn){ze("WebGLRenderer: setEffects() requires outputBufferType set to HalfFloatType or FloatType.");return}if(R){for(let z=0;z<R.length;z++)if(R[z].isOutputPass===!0){Ue("WebGLRenderer: OutputPass is not needed in setEffects(). Tone mapping and color space conversion are applied automatically.");break}}C.setEffects(R||[])},this.getCurrentViewport=function(R){return R.copy(B)},this.getViewport=function(R){return R.copy(ue)},this.setViewport=function(R,z,J,Y){R.isVector4?ue.set(R.x,R.y,R.z,R.w):ue.set(R,z,J,Y),E.viewport(B.copy(ue).multiplyScalar(Z).round())},this.getScissor=function(R){return R.copy(Be)},this.setScissor=function(R,z,J,Y){R.isVector4?Be.set(R.x,R.y,R.z,R.w):Be.set(R,z,J,Y),E.scissor(Q.copy(Be).multiplyScalar(Z).round())},this.getScissorTest=function(){return ut},this.setScissorTest=function(R){E.setScissorTest(ut=R)},this.setOpaqueSort=function(R){ce=R},this.setTransparentSort=function(R){Ee=R},this.getClearColor=function(R){return R.copy(Xe.getClearColor())},this.setClearColor=function(){Xe.setClearColor(...arguments)},this.getClearAlpha=function(){return Xe.getClearAlpha()},this.setClearAlpha=function(){Xe.setClearAlpha(...arguments)},this.clear=function(R=!0,z=!0,J=!0){let Y=0;if(R){let $=!1;if(q!==null){let Se=q.texture.format;$=_.has(Se)}if($){let Se=q.texture.type,Te=m.has(Se),xe=Xe.getClearColor(),Ce=Xe.getClearAlpha(),Le=xe.r,qe=xe.g,Ze=xe.b;Te?(w[0]=Le,w[1]=qe,w[2]=Ze,w[3]=Ce,H.clearBufferuiv(H.COLOR,0,w)):(T[0]=Le,T[1]=qe,T[2]=Ze,T[3]=Ce,H.clearBufferiv(H.COLOR,0,T))}else Y|=H.COLOR_BUFFER_BIT}z&&(Y|=H.DEPTH_BUFFER_BIT,this.state.buffers.depth.setMask(!0)),J&&(Y|=H.STENCIL_BUFFER_BIT,this.state.buffers.stencil.setMask(4294967295)),Y!==0&&H.clear(Y)},this.clearColor=function(){this.clear(!0,!1,!1)},this.clearDepth=function(){this.clear(!1,!0,!1)},this.clearStencil=function(){this.clear(!1,!1,!0)},this.setNodesHandler=function(R){R.setRenderer(this),O=R},this.dispose=function(){n.removeEventListener("webglcontextlost",ft,!1),n.removeEventListener("webglcontextrestored",nt,!1),n.removeEventListener("webglcontextcreationerror",Hn,!1),Xe.dispose(),ve.dispose(),me.dispose(),K.dispose(),he.dispose(),se.dispose(),Me.dispose(),ae.dispose(),pe.dispose(),Ie.dispose(),Ie.removeEventListener("sessionstart",nm),Ie.removeEventListener("sessionend",im),Mr.stop()};function ft(R){R.preventDefault(),Bf("WebGLRenderer: Context Lost."),P=!0}function nt(){Bf("WebGLRenderer: Context Restored."),P=!1;let R=X.autoReset,z=Fe.enabled,J=Fe.autoUpdate,Y=Fe.needsUpdate,$=Fe.type;Ne(),X.autoReset=R,Fe.enabled=z,Fe.autoUpdate=J,Fe.needsUpdate=Y,Fe.type=$}function Hn(R){ze("WebGLRenderer: A WebGL context could not be created. Reason: ",R.statusMessage)}function ai(R){let z=R.target;z.removeEventListener("dispose",ai),jb(z)}function jb(R){Yb(R),K.remove(R)}function Yb(R){let z=K.get(R).programs;z!==void 0&&(z.forEach(function(J){pe.releaseProgram(J)}),R.isShaderMaterial&&pe.releaseShaderCache(R))}this.renderBufferDirect=function(R,z,J,Y,$,Se){z===null&&(z=fn);let Te=$.isMesh&&$.matrixWorld.determinantAffine()<0,xe=Kb(R,z,J,Y,$);E.setMaterial(Y,Te);let Ce=J.index,Le=1;if(Y.wireframe===!0){if(Ce=ne.getWireframeAttribute(J),Ce===void 0)return;Le=2}let qe=J.drawRange,Ze=J.attributes.position,Re=qe.start*Le,it=(qe.start+qe.count)*Le;Se!==null&&(Re=Math.max(Re,Se.start*Le),it=Math.min(it,(Se.start+Se.count)*Le)),Ce!==null?(Re=Math.max(Re,0),it=Math.min(it,Ce.count)):Ze!=null&&(Re=Math.max(Re,0),it=Math.min(it,Ze.count));let Tt=it-Re;if(Tt<0||Tt===1/0)return;Me.setup($,Y,xe,J,Ce);let mt,ht=ge;if(Ce!==null&&(mt=de.get(Ce),ht=ie,ht.setIndex(mt)),$.isMesh)Y.wireframe===!0?(E.setLineWidth(Y.wireframeLinewidth*At()),ht.setMode(H.LINES)):ht.setMode(H.TRIANGLES);else if($.isLine){let qt=Y.linewidth;qt===void 0&&(qt=1),E.setLineWidth(qt*At()),$.isLineSegments?ht.setMode(H.LINES):$.isLineLoop?ht.setMode(H.LINE_LOOP):ht.setMode(H.LINE_STRIP)}else $.isPoints?ht.setMode(H.POINTS):$.isSprite&&ht.setMode(H.TRIANGLES);if($.isBatchedMesh)if(at.get("WEBGL_multi_draw"))ht.renderMultiDraw($._multiDrawStarts,$._multiDrawCounts,$._multiDrawCount);else{let qt=$._multiDrawStarts,Ae=$._multiDrawCounts,rn=$._multiDrawCount,Qe=Ce?de.get(Ce).bytesPerElement:1,Rn=K.get(Y).currentProgram.getUniforms();for(let li=0;li<rn;li++)Rn.setValue(H,"_gl_DrawID",li),ht.render(qt[li]/Qe,Ae[li])}else if($.isInstancedMesh)ht.renderInstances(Re,Tt,$.count);else if(J.isInstancedBufferGeometry){let qt=J._maxInstanceCount!==void 0?J._maxInstanceCount:1/0,Ae=Math.min(J.instanceCount,qt);ht.renderInstances(Re,Tt,Ae)}else ht.render(Re,Tt)};function tm(R,z,J,Y){O!==null&&R.isNodeMaterial&&O.setObject(Y,R),$e===!0&&Oe.setState(R,J,!1),R.transparent===!0&&R.side===di&&R.forceSinglePass===!1?(R.side=Gt,R.needsUpdate=!0,Qa(R,z,Y),R.side=ir,R.needsUpdate=!0,Qa(R,z,Y),R.side=di):Qa(R,z,Y)}this.compile=function(R,z,J=null){J===null&&(J=R),O!==null&&O.renderStart(R,z,J),S=me.get(J),S.init(z),x.push(S),J.traverseVisible(function($){$.isLight&&$.layers.test(z.layers)&&(S.pushLight($),$.castShadow&&S.pushShadow($))}),R!==J&&R.traverseVisible(function($){$.isLight&&$.layers.test(z.layers)&&(S.pushLight($),$.castShadow&&S.pushShadow($))}),S.setupLights(),O!==null&&O.updateLights(S.state.lightsArray),Je=this.localClippingEnabled,$e=Oe.init(this.clippingPlanes,Je),$e===!0&&Oe.setGlobalState(this.clippingPlanes,z),O!==null&&Fe.render(S.state.shadowsArray,J,z);let Y=new Set;return R.traverse(function($){if(!($.isMesh||$.isPoints||$.isLine||$.isSprite))return;let Se=$.material;if(Se)if(Array.isArray(Se))for(let Te=0;Te<Se.length;Te++){let xe=Se[Te];tm(xe,J,z,$),Y.add(xe)}else tm(Se,J,z,$),Y.add(Se)}),S=x.pop(),O!==null&&O.renderEnd(),Y},this.compileAsync=function(R,z,J=null){let Y=this.compile(R,z,J);return new Promise($=>{function Se(){if(Y.forEach(function(Te){let Ce=K.get(Te).currentProgram;(Ce===void 0||Ce.isReady())&&Y.delete(Te)}),Y.size===0){$(R);return}setTimeout(Se,10)}at.get("KHR_parallel_shader_compile")!==null?Se():setTimeout(Se,10)})};let Nh=null;function $b(R){Nh&&Nh(R)}function nm(){Mr.stop()}function im(){Mr.start()}let Mr=new Zg;Mr.setAnimationLoop($b),typeof self<"u"&&Mr.setContext(self),this.setAnimationLoop=function(R){Nh=R,Ie.setAnimationLoop(R),R===null?Mr.stop():Mr.start()},Ie.addEventListener("sessionstart",nm),Ie.addEventListener("sessionend",im),this.render=function(R,z){if(z!==void 0&&z.isCamera!==!0){ze("WebGLRenderer.render: camera is not an instance of THREE.Camera.");return}if(P===!0)return;O!==null&&O.renderStart(R,z);let J=Ie.enabled===!0&&Ie.isPresenting===!0,Y=C!==null&&(q===null||J)&&C.begin(L,q);if(R.matrixWorldAutoUpdate===!0&&R.updateMatrixWorld(),z.parent===null&&z.matrixWorldAutoUpdate===!0&&z.updateMatrixWorld(),Ie.enabled===!0&&Ie.isPresenting===!0&&(C===null||C.isCompositing()===!1)&&(Ie.cameraAutoUpdate===!0&&Ie.updateCamera(z),z=Ie.getCamera()),R.isScene===!0&&R.onBeforeRender(L,R,z,q),S=me.get(R,x.length),S.init(z),S.state.textureUnits=te.getTextureUnits(),x.push(S),Ge.multiplyMatrices(z.projectionMatrix,z.matrixWorldInverse),Ve.setFromProjectionMatrix(Ge,jn,z.reversedDepth),Je=this.localClippingEnabled,$e=Oe.init(this.clippingPlanes,Je),b=ve.get(R,A.length),b.init(),A.push(b),Ie.enabled===!0&&Ie.isPresenting===!0){let Te=L.xr.getDepthSensingMesh();Te!==null&&Uh(Te,z,-1/0,L.sortObjects)}Uh(R,z,0,L.sortObjects),b.finish(),O!==null&&O.updateLights(S.state.lightsArray),L.sortObjects===!0&&b.sort(ce,Ee),St=Ie.enabled===!1||Ie.isPresenting===!1||Ie.hasDepthSensing()===!1,St&&Xe.addToRenderList(b,R),this.info.render.frame++,this.info.autoReset===!0&&this.info.reset(),$e===!0&&Oe.beginShadows();let $=S.state.shadowsArray;if(Fe.render($,R,z),$e===!0&&Oe.endShadows(),(Y&&C.hasRenderPass())===!1){let Te=b.opaque,xe=b.transmissive;if(S.setupLights(),z.isArrayCamera){let Ce=z.cameras;if(xe.length>0)for(let Le=0,qe=Ce.length;Le<qe;Le++){let Ze=Ce[Le];sm(Te,xe,R,Ze)}St&&Xe.render(R);for(let Le=0,qe=Ce.length;Le<qe;Le++){let Ze=Ce[Le];rm(b,R,Ze,Ze.viewport)}}else xe.length>0&&sm(Te,xe,R,z),St&&Xe.render(R),rm(b,R,z)}q!==null&&k===0&&(te.updateMultisampleRenderTarget(q),te.updateRenderTargetMipmap(q)),Y&&C.end(L),R.isScene===!0&&R.onAfterRender(L,R,z),Me.resetDefaultState(),W=-1,j=null,x.pop(),x.length>0?(S=x[x.length-1],te.setTextureUnits(S.state.textureUnits),$e===!0&&Oe.setGlobalState(L.clippingPlanes,S.state.camera)):S=null,A.pop(),A.length>0?b=A[A.length-1]:b=null,O!==null&&O.renderEnd()};function Uh(R,z,J,Y){if(R.visible===!1)return;if(R.layers.test(z.layers)){if(R.isGroup)J=R.renderOrder;else if(R.isLOD)R.autoUpdate===!0&&R.update(z);else if(R.isLightProbeGrid)S.pushLightProbeGrid(R);else if(R.isLight)S.pushLight(R),R.castShadow&&S.pushShadow(R);else if(R.isSprite){if(!R.frustumCulled||R.intersectsFrustum(Ve)){Y&&Dt.setFromMatrixPosition(R.matrixWorld).applyMatrix4(Ge);let Te=se.update(R),xe=R.material;xe.visible&&b.push(R,Te,xe,J,Dt.z,null,z)}}else if((R.isMesh||R.isLine||R.isPoints)&&(!R.frustumCulled||R.intersectsFrustum(Ve))){let Te=se.update(R),xe=R.material;if(Y&&(R.boundingSphere!==void 0?(R.boundingSphere===null&&R.computeBoundingSphere(),Dt.copy(R.boundingSphere.center)):(Te.boundingSphere===null&&Te.computeBoundingSphere(),Dt.copy(Te.boundingSphere.center)),Dt.applyMatrix4(R.matrixWorld).applyMatrix4(Ge)),Array.isArray(xe)){let Ce=Te.groups;for(let Le=0,qe=Ce.length;Le<qe;Le++){let Ze=Ce[Le],Re=xe[Ze.materialIndex];Re&&Re.visible&&b.push(R,Te,Re,J,Dt.z,Ze,z)}}else xe.visible&&b.push(R,Te,xe,J,Dt.z,null,z)}}let Se=R.children;for(let Te=0,xe=Se.length;Te<xe;Te++)Uh(Se[Te],z,J,Y)}function rm(R,z,J,Y){let{opaque:$,transmissive:Se,transparent:Te}=R;S.setupLightsView(J),$e===!0&&Oe.setGlobalState(L.clippingPlanes,J),Y&&E.viewport(B.copy(Y)),$.length>0&&Ja($,z,J),Se.length>0&&Ja(Se,z,J),Te.length>0&&Ja(Te,z,J),E.buffers.depth.setTest(!0),E.buffers.depth.setMask(!0),E.buffers.color.setMask(!0),E.setPolygonOffset(!1)}function sm(R,z,J,Y){if((J.isScene===!0?J.overrideMaterial:null)!==null)return;if(S.state.transmissionRenderTarget[Y.id]===void 0){let Re=at.has("EXT_color_buffer_half_float")||at.has("EXT_color_buffer_float");S.state.transmissionRenderTarget[Y.id]=new Zt(1,1,{generateMipmaps:!0,type:Re?En:mn,minFilter:sr,samples:Math.max(4,N.samples),stencilBuffer:o,resolveDepthBuffer:!1,resolveStencilBuffer:!1,storeMultisampledDepthBuffer:!1,storeMultisampledStencilBuffer:!1,colorSpace:Ke.workingColorSpace})}let Se=S.state.transmissionRenderTarget[Y.id],Te=Y.viewport||B;Se.setSize(Te.z*L.transmissionResolutionScale,Te.w*L.transmissionResolutionScale);let xe=L.getRenderTarget(),Ce=L.getActiveCubeFace(),Le=L.getActiveMipmapLevel();L.setRenderTarget(Se),L.getClearColor(be),ee=L.getClearAlpha(),ee<1&&L.setClearColor(16777215,.5),L.clear(),St&&Xe.render(J);let qe=L.toneMapping;L.toneMapping=Zn;let Ze=Y.viewport;if(Y.viewport!==void 0&&(Y.viewport=void 0),S.setupLightsView(Y),$e===!0&&Oe.setGlobalState(L.clippingPlanes,Y),Ja(R,J,Y),te.updateMultisampleRenderTarget(Se),te.updateRenderTargetMipmap(Se),at.has("WEBGL_multisampled_render_to_texture")===!1){let Re=!1;for(let it=0,Tt=z.length;it<Tt;it++){let mt=z[it],{object:ht,geometry:qt,material:Ae,group:rn}=mt;if(Ae.side===di&&ht.layers.test(Y.layers)){let Qe=Ae.side;Ae.side=Gt,Ae.needsUpdate=!0,om(ht,J,Y,qt,Ae,rn),Ae.side=Qe,Ae.needsUpdate=!0,Re=!0}}Re===!0&&(te.updateMultisampleRenderTarget(Se),te.updateRenderTargetMipmap(Se))}L.setRenderTarget(xe,Ce,Le),L.setClearColor(be,ee),Ze!==void 0&&(Y.viewport=Ze),L.toneMapping=qe}function Ja(R,z,J){let Y=z.isScene===!0?z.overrideMaterial:null;for(let $=0,Se=R.length;$<Se;$++){let Te=R[$],{object:xe,geometry:Ce,group:Le}=Te,qe=Te.material;qe.allowOverride===!0&&Y!==null&&(qe=Y),xe.layers.test(J.layers)&&om(xe,z,J,Ce,qe,Le)}}function om(R,z,J,Y,$,Se){O!==null&&$.isNodeMaterial&&O.setObject(R,$),R.onBeforeRender(L,z,J,Y,$,Se),R.modelViewMatrix.multiplyMatrices(J.matrixWorldInverse,R.matrixWorld),R.normalMatrix.getNormalMatrix(R.modelViewMatrix),$.onBeforeRender(L,z,J,Y,R,Se),$.transparent===!0&&$.side===di&&$.forceSinglePass===!1?($.side=Gt,$.needsUpdate=!0,L.renderBufferDirect(J,z,Y,$,R,Se),$.side=ir,$.needsUpdate=!0,L.renderBufferDirect(J,z,Y,$,R,Se),$.side=di):L.renderBufferDirect(J,z,Y,$,R,Se),R.onAfterRender(L,z,J,Y,$,Se)}function Qa(R,z,J){z.isScene!==!0&&(z=fn);let Y=K.get(R),$=S.state.lights,Se=S.state.shadowsArray,Te=$.state.version,xe=pe.getParameters(R,$.state,Se,z,J,S.state.lightProbeGridArray),Ce=pe.getProgramCacheKey(xe),Le=Y.programs;Y.environment=R.isMeshStandardMaterial||R.isMeshLambertMaterial||R.isMeshPhongMaterial?z.environment:null,Y.fog=z.fog;let qe=R.isMeshStandardMaterial||R.isMeshLambertMaterial&&!R.envMap||R.isMeshPhongMaterial&&!R.envMap;Y.envMap=he.get(R.envMap||Y.environment,qe),Y.envMapRotation=Y.environment!==null&&R.envMap===null?z.environmentRotation:R.envMapRotation,Le===void 0&&(R.addEventListener("dispose",ai),Le=new Map,Y.programs=Le);let Ze=Le.get(Ce);if(Ze!==void 0){if(Y.currentProgram===Ze&&Y.lightsStateVersion===Te)return lm(R,xe),Ze}else xe.uniforms=pe.getUniforms(R),O!==null&&R.isNodeMaterial&&O.build(R,J,xe),R.onBeforeCompile(xe,L),Ze=pe.acquireProgram(xe,Ce),Le.set(Ce,Ze),Y.uniforms=xe.uniforms;let Re=Y.uniforms;return(!R.isShaderMaterial&&!R.isRawShaderMaterial||R.clipping===!0)&&(Re.clippingPlanes=Oe.uniform),lm(R,xe),Y.needsLights=Qb(R),Y.lightsStateVersion=Te,Y.needsLights&&(Re.ambientLightColor.value=$.state.ambient,Re.lightProbe.value=$.state.probe,Re.sunLights.value=$.state.sun,Re.sunLightShadows.value=$.state.sunShadow,Re.directionalLights.value=$.state.directional,Re.directionalLightShadows.value=$.state.directionalShadow,Re.spotLights.value=$.state.spot,Re.spotLightShadows.value=$.state.spotShadow,Re.rectAreaLights.value=$.state.rectArea,Re.ltc_1.value=$.state.rectAreaLTC1,Re.ltc_2.value=$.state.rectAreaLTC2,Re.pointLights.value=$.state.point,Re.pointLightShadows.value=$.state.pointShadow,Re.hemisphereLights.value=$.state.hemi,Re.sunShadowMatrix.value=$.state.sunShadowMatrix,Re.sunShadowCascade.value=$.state.sunShadowCascade,Re.directionalShadowMatrix.value=$.state.directionalShadowMatrix,Re.spotLightMatrix.value=$.state.spotLightMatrix,Re.spotLightMap.value=$.state.spotLightMap,Re.pointShadowMatrix.value=$.state.pointShadowMatrix),Y.lightProbeGrid=S.state.lightProbeGridArray.length>0,Y.currentProgram=Ze,Y.uniformsList=null,Ze}function am(R){if(R.uniformsList===null){let z=R.currentProgram.getUniforms();R.uniformsList=Xs.seqWithValue(z.seq,R.uniforms)}return R.uniformsList}function lm(R,z){let J=K.get(R);J.outputColorSpace=z.outputColorSpace,J.batching=z.batching,J.batchingColor=z.batchingColor,J.instancing=z.instancing,J.instancingColor=z.instancingColor,J.instancingMorph=z.instancingMorph,J.skinning=z.skinning,J.morphTargets=z.morphTargets,J.morphNormals=z.morphNormals,J.morphColors=z.morphColors,J.morphTargetsCount=z.morphTargetsCount,J.numClippingPlanes=z.numClippingPlanes,J.numIntersection=z.numClipIntersection,J.vertexAlphas=z.vertexAlphas,J.vertexTangents=z.vertexTangents,J.toneMapping=z.toneMapping}function Zb(R,z){if(R.length===0)return null;if(R.length===1)return R[0].texture!==null?R[0]:null;y.setFromMatrixPosition(z.matrixWorld);for(let J=0,Y=R.length;J<Y;J++){let $=R[J];if($.texture!==null&&$.boundingBox.containsPoint(y))return $}return null}function Kb(R,z,J,Y,$){z.isScene!==!0&&(z=fn),te.resetTextureUnits();let Se=z.fog,Te=Y.isMeshStandardMaterial||Y.isMeshLambertMaterial||Y.isMeshPhongMaterial?z.environment:null,xe=q===null?L.outputColorSpace:q.isXRRenderTarget===!0?q.texture.colorSpace:Ke.workingColorSpace,Ce=Y.isMeshStandardMaterial||Y.isMeshLambertMaterial&&!Y.envMap||Y.isMeshPhongMaterial&&!Y.envMap,Le=he.get(Y.envMap||Te,Ce),qe=Y.vertexColors===!0&&!!J.attributes.color&&J.attributes.color.itemSize===4,Ze=!!J.attributes.tangent&&(!!Y.normalMap||Y.anisotropy>0),Re=!!J.morphAttributes.position,it=!!J.morphAttributes.normal,Tt=!!J.morphAttributes.color,mt=Zn;Y.toneMapped&&(q===null||q.isXRRenderTarget===!0)&&(mt=L.toneMapping);let ht=J.morphAttributes.position||J.morphAttributes.normal||J.morphAttributes.color,qt=ht!==void 0?ht.length:0,Ae=K.get(Y),rn=S.state.lights;if($e===!0&&(Je===!0||R!==j)){let dt=R===j&&Y.id===W;Oe.setState(Y,R,dt)}let Qe=!1;Y.version===Ae.__version?(Ae.needsLights&&Ae.lightsStateVersion!==rn.state.version||Ae.outputColorSpace!==xe||$.isBatchedMesh&&Ae.batching===!1||!$.isBatchedMesh&&Ae.batching===!0||$.isBatchedMesh&&Ae.batchingColor===!0&&$._colorsTexture===null||$.isBatchedMesh&&Ae.batchingColor===!1&&$._colorsTexture!==null||$.isInstancedMesh&&Ae.instancing===!1||!$.isInstancedMesh&&Ae.instancing===!0||$.isSkinnedMesh&&Ae.skinning===!1||!$.isSkinnedMesh&&Ae.skinning===!0||$.isInstancedMesh&&Ae.instancingColor===!0&&$.instanceColor===null||$.isInstancedMesh&&Ae.instancingColor===!1&&$.instanceColor!==null||$.isInstancedMesh&&Ae.instancingMorph===!0&&$.morphTexture===null||$.isInstancedMesh&&Ae.instancingMorph===!1&&$.morphTexture!==null||Ae.envMap!==Le||Y.fog===!0&&Ae.fog!==Se||Ae.numClippingPlanes!==void 0&&(Ae.numClippingPlanes!==Oe.numPlanes||Ae.numIntersection!==Oe.numIntersection)||Ae.vertexAlphas!==qe||Ae.vertexTangents!==Ze||Ae.morphTargets!==Re||Ae.morphNormals!==it||Ae.morphColors!==Tt||Ae.toneMapping!==mt||Ae.morphTargetsCount!==qt||!!Ae.lightProbeGrid!=S.state.lightProbeGridArray.length>0)&&(Qe=!0):(Qe=!0,Ae.__version=Y.version);let Rn=Ae.currentProgram;Qe===!0&&(Rn=Qa(Y,z,$),O&&Y.isNodeMaterial&&O.onUpdateProgram(Y,Rn,Ae));let li=!1,Gi=!1,rs=!1,ct=Rn.getUniforms(),Mt=Ae.uniforms;if(E.useProgram(Rn.program)&&(li=!0,Gi=!0,rs=!0),Y.id!==W&&(W=Y.id,Gi=!0),Ae.needsLights){let dt=Zb(S.state.lightProbeGridArray,$);Ae.lightProbeGrid!==dt&&(Ae.lightProbeGrid=dt,Gi=!0)}if(li||j!==R){E.buffers.depth.getReversed()&&R.reversedDepth!==!0&&(R._reversedDepth=!0,R.updateProjectionMatrix()),ct.setValue(H,"projectionMatrix",R.projectionMatrix),ct.setValue(H,"viewMatrix",R.matrixWorldInverse);let Wi=ct.map.cameraPosition;Wi!==void 0&&Wi.setValue(H,tt.setFromMatrixPosition(R.matrixWorld)),N.logarithmicDepthBuffer&&ct.setValue(H,"logDepthBufFC",2/(Math.log(R.far+1)/Math.LN2)),(Y.isMeshPhongMaterial||Y.isMeshToonMaterial||Y.isMeshLambertMaterial||Y.isMeshBasicMaterial||Y.isMeshStandardMaterial||Y.isShaderMaterial)&&ct.setValue(H,"isOrthographic",R.isOrthographicCamera===!0),j!==R&&(j=R,Gi=!0,rs=!0)}if(Ae.needsLights&&(rn.state.sunShadowMap.length>0&&ct.setValue(H,"sunShadowMap",rn.state.sunShadowMap,te),rn.state.directionalShadowMap.length>0&&ct.setValue(H,"directionalShadowMap",rn.state.directionalShadowMap,te),rn.state.spotShadowMap.length>0&&ct.setValue(H,"spotShadowMap",rn.state.spotShadowMap,te),rn.state.pointShadowMap.length>0&&ct.setValue(H,"pointShadowMap",rn.state.pointShadowMap,te)),$.isSkinnedMesh){ct.setOptional(H,$,"bindMatrix"),ct.setOptional(H,$,"bindMatrixInverse");let dt=$.skeleton;dt&&(dt.boneTexture===null&&dt.computeBoneTexture(),ct.setValue(H,"boneTexture",dt.boneTexture,te))}$.isBatchedMesh&&(ct.setOptional(H,$,"batchingTexture"),ct.setValue(H,"batchingTexture",$._matricesTexture,te),ct.setOptional(H,$,"batchingIdTexture"),ct.setValue(H,"batchingIdTexture",$._indirectTexture,te),ct.setOptional(H,$,"batchingColorTexture"),$._colorsTexture!==null&&ct.setValue(H,"batchingColorTexture",$._colorsTexture,te));let Hi=J.morphAttributes;if((Hi.position!==void 0||Hi.normal!==void 0||Hi.color!==void 0)&&G.update($,J,Rn),(Gi||Ae.receiveShadow!==$.receiveShadow)&&(Ae.receiveShadow=$.receiveShadow,ct.setValue(H,"receiveShadow",$.receiveShadow)),(Y.isMeshStandardMaterial||Y.isMeshLambertMaterial||Y.isMeshPhongMaterial)&&Y.envMap===null&&z.environment!==null&&(Mt.envMapIntensity.value=z.environmentIntensity),Mt.dfgLUT!==void 0&&(Mt.dfgLUT.value=eT()),Gi){if(ct.setValue(H,"toneMappingExposure",L.toneMappingExposure),Ae.needsLights&&Jb(Mt,rs),Se&&Y.fog===!0&&De.refreshFogUniforms(Mt,Se),De.refreshMaterialUniforms(Mt,Y,Z,V,S.state.transmissionRenderTarget[R.id]),Ae.needsLights&&Ae.lightProbeGrid){let dt=Ae.lightProbeGrid;Mt.probesSH.value=dt.texture,Mt.probesMin.value.copy(dt.boundingBox.min),Mt.probesMax.value.copy(dt.boundingBox.max),Mt.probesResolution.value.copy(dt.resolution)}Xs.upload(H,am(Ae),Mt,te)}if(Y.isShaderMaterial&&Y.uniformsNeedUpdate===!0&&(Xs.upload(H,am(Ae),Mt,te),Y.uniformsNeedUpdate=!1),Y.isSpriteMaterial&&ct.setValue(H,"center",$.center),ct.setValue(H,"modelViewMatrix",$.modelViewMatrix),ct.setValue(H,"normalMatrix",$.normalMatrix),ct.setValue(H,"modelMatrix",$.matrixWorld),Y.uniformsGroups!==void 0){let dt=Y.uniformsGroups;for(let Wi=0,ss=dt.length;Wi<ss;Wi++){let um=dt[Wi];ae.update(um,Rn),ae.bind(um,Rn)}}return Rn}function Jb(R,z){R.ambientLightColor.needsUpdate=z,R.lightProbe.needsUpdate=z,R.sunLights.needsUpdate=z,R.sunLightShadows.needsUpdate=z,R.directionalLights.needsUpdate=z,R.directionalLightShadows.needsUpdate=z,R.pointLights.needsUpdate=z,R.pointLightShadows.needsUpdate=z,R.spotLights.needsUpdate=z,R.spotLightShadows.needsUpdate=z,R.rectAreaLights.needsUpdate=z,R.hemisphereLights.needsUpdate=z}function Qb(R){return R.isMeshLambertMaterial||R.isMeshToonMaterial||R.isMeshPhongMaterial||R.isMeshStandardMaterial||R.isShadowMaterial||R.isShaderMaterial&&R.lights===!0}this.getActiveCubeFace=function(){return D},this.getActiveMipmapLevel=function(){return k},this.getRenderTarget=function(){return q},this.setRenderTargetTextures=function(R,z,J){let Y=K.get(R);Y.__autoAllocateDepthBuffer=R.resolveDepthBuffer===!1,Y.__autoAllocateDepthBuffer===!1&&(Y.__useRenderToTexture=!1),K.get(R.texture).__webglTexture=z,K.get(R.depthTexture).__webglTexture=Y.__autoAllocateDepthBuffer?void 0:J,Y.__hasExternalTextures=!0},this.setRenderTargetFramebuffer=function(R,z){let J=K.get(R);J.__webglFramebuffer=z,J.__useDefaultFramebuffer=z===void 0},this.setRenderTarget=function(R,z=0,J=0){q=R,D=z,k=J;let Y=null,$=!1,Se=!1;if(R){let xe=K.get(R);if(xe.__useDefaultFramebuffer!==void 0){E.bindFramebuffer(H.FRAMEBUFFER,xe.__webglFramebuffer),B.copy(R.viewport),Q.copy(R.scissor),re=R.scissorTest,E.viewport(B),E.scissor(Q),E.setScissorTest(re),W=-1;return}else if(xe.__webglFramebuffer===void 0)te.setupRenderTarget(R);else if(xe.__hasExternalTextures)te.rebindTextures(R,K.get(R.texture).__webglTexture,K.get(R.depthTexture).__webglTexture);else if(R.depthBuffer){let qe=R.depthTexture;if(xe.__boundDepthTexture!==qe){if(qe!==null&&K.has(qe)&&(R.width!==qe.image.width||R.height!==qe.image.height))throw new Error("THREE.WebGLRenderer: Attached DepthTexture is initialized to the incorrect size.");te.setupDepthRenderbuffer(R)}}let Ce=R.texture;(Ce.isData3DTexture||Ce.isDataArrayTexture||Ce.isCompressedArrayTexture)&&(Se=!0);let Le=K.get(R).__webglFramebuffer;R.isWebGLCubeRenderTarget?(Array.isArray(Le[z])?Y=Le[z][J]:Y=Le[z],$=!0):R.samples>0&&te.useMultisampledRTT(R)===!1?Y=K.get(R).__webglMultisampledFramebuffer:Array.isArray(Le)?Y=Le[J]:Y=Le,B.copy(R.viewport),Q.copy(R.scissor),re=R.scissorTest}else B.copy(ue).multiplyScalar(Z).floor(),Q.copy(Be).multiplyScalar(Z).floor(),re=ut;if(J!==0&&(Y=U),E.bindFramebuffer(H.FRAMEBUFFER,Y)&&E.drawBuffers(R,Y),E.viewport(B),E.scissor(Q),E.setScissorTest(re),$){let xe=K.get(R.texture);H.framebufferTexture2D(H.FRAMEBUFFER,H.COLOR_ATTACHMENT0,H.TEXTURE_CUBE_MAP_POSITIVE_X+z,xe.__webglTexture,J)}else if(Se){let xe=z;for(let Ce=0;Ce<R.textures.length;Ce++){let Le=K.get(R.textures[Ce]);H.framebufferTextureLayer(H.FRAMEBUFFER,H.COLOR_ATTACHMENT0+Ce,Le.__webglTexture,J,xe)}}else if(R!==null&&J!==0){let xe=K.get(R.texture);H.framebufferTexture2D(H.FRAMEBUFFER,H.COLOR_ATTACHMENT0,H.TEXTURE_2D,xe.__webglTexture,J)}W=-1};function cm(R){let z=K.get(R);return(z.__readFormat!==R.format||z.__readType!==R.type)&&(z.__readFormat=R.format,z.__readType=R.type,z.__formatReadable=N.textureFormatReadable(R.format),z.__typeReadable=N.textureTypeReadable(R.type)),z}this.readRenderTargetPixels=function(R,z,J,Y,$,Se,Te,xe=0){if(!(R&&R.isWebGLRenderTarget)){ze("WebGLRenderer.readRenderTargetPixels: renderTarget is not THREE.WebGLRenderTarget.");return}let Ce=K.get(R).__webglFramebuffer;if(R.isWebGLCubeRenderTarget&&Te!==void 0&&(Ce=Ce[Te]),Ce){E.bindFramebuffer(H.FRAMEBUFFER,Ce);try{let Le=R.textures[xe],qe=Le.format,Ze=Le.type;R.textures.length>1&&H.readBuffer(H.COLOR_ATTACHMENT0+xe);let Re=cm(Le);if(Re.__formatReadable===!1){ze("WebGLRenderer.readRenderTargetPixels: renderTarget is not in RGBA or implementation defined format.");return}if(Re.__typeReadable===!1){ze("WebGLRenderer.readRenderTargetPixels: renderTarget is not in UnsignedByteType or implementation defined type.");return}z>=0&&z<=R.width-Y&&J>=0&&J<=R.height-$&&H.readPixels(z,J,Y,$,_e.convert(qe),_e.convert(Ze),Se)}finally{let Le=q!==null?K.get(q).__webglFramebuffer:null;E.bindFramebuffer(H.FRAMEBUFFER,Le)}}},this.readRenderTargetPixelsAsync=async function(R,z,J,Y,$,Se,Te,xe=0){if(!(R&&R.isWebGLRenderTarget))throw new Error("THREE.WebGLRenderer.readRenderTargetPixels: renderTarget is not THREE.WebGLRenderTarget.");let Ce=K.get(R).__webglFramebuffer;if(R.isWebGLCubeRenderTarget&&Te!==void 0&&(Ce=Ce[Te]),Ce)if(z>=0&&z<=R.width-Y&&J>=0&&J<=R.height-$){E.bindFramebuffer(H.FRAMEBUFFER,Ce);let Le=R.textures[xe],qe=Le.format,Ze=Le.type;R.textures.length>1&&H.readBuffer(H.COLOR_ATTACHMENT0+xe);let Re=cm(Le);if(Re.__formatReadable===!1)throw new Error("THREE.WebGLRenderer.readRenderTargetPixelsAsync: renderTarget is not in RGBA or implementation defined format.");if(Re.__typeReadable===!1)throw new Error("THREE.WebGLRenderer.readRenderTargetPixelsAsync: renderTarget is not in UnsignedByteType or implementation defined type.");let it=H.createBuffer();H.bindBuffer(H.PIXEL_PACK_BUFFER,it),H.bufferData(H.PIXEL_PACK_BUFFER,Se.byteLength,H.STREAM_READ),H.readPixels(z,J,Y,$,_e.convert(qe),_e.convert(Ze),0),H.bindBuffer(H.PIXEL_PACK_BUFFER,null);let Tt=q!==null?K.get(q).__webglFramebuffer:null;E.bindFramebuffer(H.FRAMEBUFFER,Tt);let mt=H.fenceSync(H.SYNC_GPU_COMMANDS_COMPLETE,0);return H.flush(),await Mg(H,mt,4),H.bindBuffer(H.PIXEL_PACK_BUFFER,it),H.getBufferSubData(H.PIXEL_PACK_BUFFER,0,Se),H.bindBuffer(H.PIXEL_PACK_BUFFER,null),H.deleteBuffer(it),H.deleteSync(mt),Se}else throw new Error("THREE.WebGLRenderer.readRenderTargetPixelsAsync: requested read bounds are out of range.")},this.copyFramebufferToTexture=function(R,z=null,J=0){let Y=Math.pow(2,-J),$=Math.floor(R.image.width*Y),Se=Math.floor(R.image.height*Y),Te=z!==null?z.x:0,xe=z!==null?z.y:0;te.setTexture2D(R,0),H.copyTexSubImage2D(H.TEXTURE_2D,J,0,0,Te,xe,$,Se),E.unbindTexture()},this.copyTextureToTexture=function(R,z,J=null,Y=null,$=0,Se=0){let Te,xe,Ce,Le,qe,Ze,Re,it,Tt,mt=R.isCompressedTexture?R.mipmaps[Se]:R.image;if(J!==null)Te=J.max.x-J.min.x,xe=J.max.y-J.min.y,Ce=J.isBox3?J.max.z-J.min.z:1,Le=J.min.x,qe=J.min.y,Ze=J.isBox3?J.min.z:0;else{let Mt=Math.pow(2,-$);Te=Math.floor(mt.width*Mt),xe=Math.floor(mt.height*Mt),R.isDataArrayTexture?Ce=mt.depth:R.isData3DTexture?Ce=Math.floor(mt.depth*Mt):Ce=1,Le=0,qe=0,Ze=0}Y!==null?(Re=Y.x,it=Y.y,Tt=Y.z):(Re=0,it=0,Tt=0);let ht=_e.convert(z.format),qt=_e.convert(z.type),Ae;z.isData3DTexture?(te.setTexture3D(z,0),Ae=H.TEXTURE_3D):z.isDataArrayTexture||z.isCompressedArrayTexture?(te.setTexture2DArray(z,0),Ae=H.TEXTURE_2D_ARRAY):(te.setTexture2D(z,0),Ae=H.TEXTURE_2D),E.activeTexture(H.TEXTURE0),E.pixelStorei(H.UNPACK_FLIP_Y_WEBGL,z.flipY),E.pixelStorei(H.UNPACK_PREMULTIPLY_ALPHA_WEBGL,z.premultiplyAlpha),E.pixelStorei(H.UNPACK_ALIGNMENT,z.unpackAlignment);let rn=E.getParameter(H.UNPACK_ROW_LENGTH),Qe=E.getParameter(H.UNPACK_IMAGE_HEIGHT),Rn=E.getParameter(H.UNPACK_SKIP_PIXELS),li=E.getParameter(H.UNPACK_SKIP_ROWS),Gi=E.getParameter(H.UNPACK_SKIP_IMAGES);E.pixelStorei(H.UNPACK_ROW_LENGTH,mt.width),E.pixelStorei(H.UNPACK_IMAGE_HEIGHT,mt.height),E.pixelStorei(H.UNPACK_SKIP_PIXELS,Le),E.pixelStorei(H.UNPACK_SKIP_ROWS,qe),E.pixelStorei(H.UNPACK_SKIP_IMAGES,Ze);let rs=R.isDataArrayTexture||R.isData3DTexture,ct=z.isDataArrayTexture||z.isData3DTexture;if(R.isDepthTexture){let Mt=K.get(R),Hi=K.get(z),dt=K.get(Mt.__renderTarget),Wi=K.get(Hi.__renderTarget);E.bindFramebuffer(H.READ_FRAMEBUFFER,dt.__webglFramebuffer),E.bindFramebuffer(H.DRAW_FRAMEBUFFER,Wi.__webglFramebuffer);for(let ss=0;ss<Ce;ss++)rs&&(H.framebufferTextureLayer(H.READ_FRAMEBUFFER,H.COLOR_ATTACHMENT0,K.get(R).__webglTexture,$,Ze+ss),H.framebufferTextureLayer(H.DRAW_FRAMEBUFFER,H.COLOR_ATTACHMENT0,K.get(z).__webglTexture,Se,Tt+ss)),H.blitFramebuffer(Le,qe,Te,xe,Re,it,Te,xe,H.DEPTH_BUFFER_BIT,H.NEAREST);E.bindFramebuffer(H.READ_FRAMEBUFFER,null),E.bindFramebuffer(H.DRAW_FRAMEBUFFER,null)}else if($!==0||R.isRenderTargetTexture||K.has(R)){let Mt=K.get(R),Hi=K.get(z);E.bindFramebuffer(H.READ_FRAMEBUFFER,M),E.bindFramebuffer(H.DRAW_FRAMEBUFFER,I);for(let dt=0;dt<Ce;dt++)rs?H.framebufferTextureLayer(H.READ_FRAMEBUFFER,H.COLOR_ATTACHMENT0,Mt.__webglTexture,$,Ze+dt):H.framebufferTexture2D(H.READ_FRAMEBUFFER,H.COLOR_ATTACHMENT0,H.TEXTURE_2D,Mt.__webglTexture,$),ct?H.framebufferTextureLayer(H.DRAW_FRAMEBUFFER,H.COLOR_ATTACHMENT0,Hi.__webglTexture,Se,Tt+dt):H.framebufferTexture2D(H.DRAW_FRAMEBUFFER,H.COLOR_ATTACHMENT0,H.TEXTURE_2D,Hi.__webglTexture,Se),$!==0?H.blitFramebuffer(Le,qe,Te,xe,Re,it,Te,xe,H.COLOR_BUFFER_BIT,H.NEAREST):ct?H.copyTexSubImage3D(Ae,Se,Re,it,Tt+dt,Le,qe,Te,xe):H.copyTexSubImage2D(Ae,Se,Re,it,Le,qe,Te,xe);E.bindFramebuffer(H.READ_FRAMEBUFFER,null),E.bindFramebuffer(H.DRAW_FRAMEBUFFER,null)}else ct?R.isDataTexture||R.isData3DTexture?H.texSubImage3D(Ae,Se,Re,it,Tt,Te,xe,Ce,ht,qt,mt.data):z.isCompressedArrayTexture?H.compressedTexSubImage3D(Ae,Se,Re,it,Tt,Te,xe,Ce,ht,mt.data):H.texSubImage3D(Ae,Se,Re,it,Tt,Te,xe,Ce,ht,qt,mt):R.isDataTexture?H.texSubImage2D(H.TEXTURE_2D,Se,Re,it,Te,xe,ht,qt,mt.data):R.isCompressedTexture?H.compressedTexSubImage2D(H.TEXTURE_2D,Se,Re,it,mt.width,mt.height,ht,mt.data):H.texSubImage2D(H.TEXTURE_2D,Se,Re,it,Te,xe,ht,qt,mt);E.pixelStorei(H.UNPACK_ROW_LENGTH,rn),E.pixelStorei(H.UNPACK_IMAGE_HEIGHT,Qe),E.pixelStorei(H.UNPACK_SKIP_PIXELS,Rn),E.pixelStorei(H.UNPACK_SKIP_ROWS,li),E.pixelStorei(H.UNPACK_SKIP_IMAGES,Gi),Se===0&&z.generateMipmaps&&H.generateMipmap(Ae),E.unbindTexture()},this.initRenderTarget=function(R){K.get(R).__webglFramebuffer===void 0&&te.setupRenderTarget(R)},this.initTexture=function(R){R.isCubeTexture?te.setTextureCube(R,0):R.isData3DTexture?te.setTexture3D(R,0):R.isDataArrayTexture||R.isCompressedArrayTexture?te.setTexture2DArray(R,0):te.setTexture2D(R,0),E.unbindTexture()},this.resetState=function(){D=0,k=0,q=null,E.reset(),Me.reset()},typeof __THREE_DEVTOOLS__<"u"&&__THREE_DEVTOOLS__.dispatchEvent(new CustomEvent("observe",{detail:this}))}get coordinateSystem(){return jn}get outputColorSpace(){return this._outputColorSpace}set outputColorSpace(e){this._outputColorSpace=e;let n=this.getContext();n.drawingBufferColorSpace=Ke._getDrawingBufferColorSpace(e),n.unpackColorSpace=Ke._getUnpackColorSpace()}};var js=new an,Ii=new fe,i0=new F,dd=new fe,lu=new fe,cu=new F,pd=new F,r0=new st,s0=new F,o0=new F,en=null,gi=null,Li=[],lr={NONE:-1,PAN:0,ROTATE:1},uu=class extends $n{constructor(e,n,r=null){super(n,r),this.objects=e,this.recursive=!0,this.transformGroup=!1,this.rotateSpeed=1,this.raycaster=new Nr,this.mouseButtons={LEFT:Et.PAN,MIDDLE:Et.PAN,RIGHT:Et.ROTATE},this.touches={ONE:Dn.PAN},this._onPointerMove=tT.bind(this),this._onPointerDown=nT.bind(this),this._onPointerCancel=iT.bind(this),this._onContextMenu=rT.bind(this),r!==null&&this.connect(r)}connect(e){super.connect(e),this.domElement.addEventListener("pointermove",this._onPointerMove),this.domElement.addEventListener("pointerdown",this._onPointerDown),this.domElement.addEventListener("pointerup",this._onPointerCancel),this.domElement.addEventListener("pointerleave",this._onPointerCancel),this.domElement.addEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction="none"}disconnect(){this.domElement.removeEventListener("pointermove",this._onPointerMove),this.domElement.removeEventListener("pointerdown",this._onPointerDown),this.domElement.removeEventListener("pointerup",this._onPointerCancel),this.domElement.removeEventListener("pointerleave",this._onPointerCancel),this.domElement.removeEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction="",this.domElement.style.cursor=""}dispose(){this.disconnect()}_updatePointer(e){let n=this.domElement.getBoundingClientRect();Ii.x=(e.clientX-n.left)/n.width*2-1,Ii.y=-(e.clientY-n.top)/n.height*2+1}_updateState(e){let n;if(e.pointerType==="touch")n=this.touches.ONE;else switch(e.button){case 0:n=this.mouseButtons.LEFT;break;case 1:n=this.mouseButtons.MIDDLE;break;case 2:n=this.mouseButtons.RIGHT;break;default:n=null}switch(n){case Et.PAN:case Dn.PAN:this.state=lr.PAN;break;case Et.ROTATE:case Dn.ROTATE:this.state=lr.ROTATE;break;default:this.state=lr.NONE}}};function tT(i){let e=this.object,n=this.domElement,r=this.raycaster;if(this.enabled!==!1){if(this._updatePointer(i),r.setFromCamera(Ii,e),en)this.state===lr.PAN?r.ray.intersectPlane(js,cu)&&(en.position.copy(cu.sub(i0).applyMatrix4(r0)),this.dispatchEvent({type:"drag",object:en})):this.state===lr.ROTATE&&(dd.subVectors(Ii,lu).multiplyScalar(this.rotateSpeed),en.rotateOnWorldAxis(s0,dd.x),en.rotateOnWorldAxis(o0.normalize(),-dd.y),this.dispatchEvent({type:"drag",object:en})),lu.copy(Ii);else if(i.pointerType==="mouse"||i.pointerType==="pen")if(Li.length=0,r.setFromCamera(Ii,e),r.intersectObjects(this.objects,this.recursive,Li),Li.length>0){let s=Li[0].object;js.setFromNormalAndCoplanarPoint(e.getWorldDirection(js.normal),pd.setFromMatrixPosition(s.matrixWorld)),gi!==s&&gi!==null&&(this.dispatchEvent({type:"hoveroff",object:gi}),n.style.cursor="auto",gi=null),gi!==s&&(this.dispatchEvent({type:"hoveron",object:s}),n.style.cursor="pointer",gi=s)}else gi!==null&&(this.dispatchEvent({type:"hoveroff",object:gi}),n.style.cursor="auto",gi=null);lu.copy(Ii)}}function nT(i){let e=this.object,n=this.domElement,r=this.raycaster;this.enabled!==!1&&(this._updatePointer(i),this._updateState(i),Li.length=0,r.setFromCamera(Ii,e),r.intersectObjects(this.objects,this.recursive,Li),Li.length>0&&(this.transformGroup===!0?en=a0(Li[0].object):en=Li[0].object,js.setFromNormalAndCoplanarPoint(e.getWorldDirection(js.normal),pd.setFromMatrixPosition(en.matrixWorld)),r.ray.intersectPlane(js,cu)&&(this.state===lr.PAN?(r0.copy(en.parent.matrixWorld).invert(),i0.copy(cu).sub(pd.setFromMatrixPosition(en.matrixWorld)),n.style.cursor="move",this.dispatchEvent({type:"dragstart",object:en})):this.state===lr.ROTATE&&(s0.set(0,1,0).applyQuaternion(e.quaternion).normalize(),o0.set(1,0,0).applyQuaternion(e.quaternion).normalize(),n.style.cursor="move",this.dispatchEvent({type:"dragstart",object:en})))),lu.copy(Ii))}function iT(){this.enabled!==!1&&(en&&(this.dispatchEvent({type:"dragend",object:en}),en=null),this.domElement.style.cursor=gi?"pointer":"auto",this.state=lr.NONE)}function rT(i){this.enabled!==!1&&i.preventDefault()}function a0(i,e=null){return i.isGroup&&(e=i),i.parent===null?e:a0(i.parent,e)}function ta(i,e,n){var r,s=1;i==null&&(i=0),e==null&&(e=0),n==null&&(n=0);function o(){var a,l=r.length,c,u=0,h=0,d=0;for(a=0;a<l;++a)c=r[a],u+=c.x||0,h+=c.y||0,d+=c.z||0;for(u=(u/l-i)*s,h=(h/l-e)*s,d=(d/l-n)*s,a=0;a<l;++a)c=r[a],u&&(c.x-=u),h&&(c.y-=h),d&&(c.z-=d)}return o.initialize=function(a){r=a},o.x=function(a){return arguments.length?(i=+a,o):i},o.y=function(a){return arguments.length?(e=+a,o):e},o.z=function(a){return arguments.length?(n=+a,o):n},o.strength=function(a){return arguments.length?(s=+a,o):s},o}function l0(i){let e=+this._x.call(null,i);return c0(this.cover(e),e,i)}function c0(i,e,n){if(isNaN(e))return i;var r,s=i._root,o={data:n},a=i._x0,l=i._x1,c,u,h,d,f;if(!s)return i._root=o,i;for(;s.length;)if((h=e>=(c=(a+l)/2))?a=c:l=c,r=s,!(s=s[d=+h]))return r[d]=o,i;if(u=+i._x.call(null,s.data),e===u)return o.next=s,r?r[d]=o:i._root=o,i;do r=r?r[d]=new Array(2):i._root=new Array(2),(h=e>=(c=(a+l)/2))?a=c:l=c;while((d=+h)==(f=+(u>=c)));return r[f]=s,r[d]=o,i}function u0(i){Array.isArray(i)||(i=Array.from(i));let e=i.length,n=new Float64Array(e),r=1/0,s=-1/0;for(let o=0,a;o<e;++o)isNaN(a=+this._x.call(null,i[o]))||(n[o]=a,a<r&&(r=a),a>s&&(s=a));if(r>s)return this;this.cover(r).cover(s);for(let o=0;o<e;++o)c0(this,n[o],i[o]);return this}function h0(i){if(isNaN(i=+i))return this;var e=this._x0,n=this._x1;if(isNaN(e))n=(e=Math.floor(i))+1;else{for(var r=n-e||1,s=this._root,o,a;e>i||i>=n;)switch(a=+(i<e),o=new Array(2),o[a]=s,s=o,r*=2,a){case 0:n=e+r;break;case 1:e=n-r;break}this._root&&this._root.length&&(this._root=s)}return this._x0=e,this._x1=n,this}function f0(){var i=[];return this.visit(function(e){if(!e.length)do i.push(e.data);while(e=e.next)}),i}function d0(i){return arguments.length?this.cover(+i[0][0]).cover(+i[1][0]):isNaN(this._x0)?void 0:[[this._x0],[this._x1]]}function Un(i,e,n){this.node=i,this.x0=e,this.x1=n}function p0(i,e){var n,r=this._x0,s,o,a=this._x1,l=[],c=this._root,u,h;for(c&&l.push(new Un(c,r,a)),e==null?e=1/0:(r=i-e,a=i+e);u=l.pop();)if(!(!(c=u.node)||(s=u.x0)>a||(o=u.x1)<r))if(c.length){var d=(s+o)/2;l.push(new Un(c[1],d,o),new Un(c[0],s,d)),(h=+(i>=d))&&(u=l[l.length-1],l[l.length-1]=l[l.length-1-h],l[l.length-1-h]=u)}else{var f=Math.abs(i-+this._x.call(null,c.data));f<e&&(e=f,r=i-f,a=i+f,n=c.data)}return n}function m0(i){if(isNaN(c=+this._x.call(null,i)))return this;var e,n=this._root,r,s,o,a=this._x0,l=this._x1,c,u,h,d,f;if(!n)return this;if(n.length)for(;;){if((h=c>=(u=(a+l)/2))?a=u:l=u,e=n,!(n=n[d=+h]))return this;if(!n.length)break;e[d+1&1]&&(r=e,f=d)}for(;n.data!==i;)if(s=n,!(n=n.next))return this;return(o=n.next)&&delete n.next,s?(o?s.next=o:delete s.next,this):e?(o?e[d]=o:delete e[d],(n=e[0]||e[1])&&n===(e[1]||e[0])&&!n.length&&(r?r[f]=n:this._root=n),this):(this._root=o,this)}function g0(i){for(var e=0,n=i.length;e<n;++e)this.remove(i[e]);return this}function _0(){return this._root}function v0(){var i=0;return this.visit(function(e){if(!e.length)do++i;while(e=e.next)}),i}function y0(i){var e=[],n,r=this._root,s,o,a;for(r&&e.push(new Un(r,this._x0,this._x1));n=e.pop();)if(!i(r=n.node,o=n.x0,a=n.x1)&&r.length){var l=(o+a)/2;(s=r[1])&&e.push(new Un(s,l,a)),(s=r[0])&&e.push(new Un(s,o,l))}return this}function x0(i){var e=[],n=[],r;for(this._root&&e.push(new Un(this._root,this._x0,this._x1));r=e.pop();){var s=r.node;if(s.length){var o,a=r.x0,l=r.x1,c=(a+l)/2;(o=s[0])&&e.push(new Un(o,a,c)),(o=s[1])&&e.push(new Un(o,c,l))}n.push(r)}for(;r=n.pop();)i(r.node,r.x0,r.x1);return this}function b0(i){return i[0]}function S0(i){return arguments.length?(this._x=i,this):this._x}function na(i,e){var n=new md(e??b0,NaN,NaN);return i==null?n:n.addAll(i)}function md(i,e,n){this._x=i,this._x0=e,this._x1=n,this._root=void 0}function w0(i){for(var e={data:i.data},n=e;i=i.next;)n=n.next={data:i.data};return e}var gn=na.prototype=md.prototype;gn.copy=function(){var i=new md(this._x,this._x0,this._x1),e=this._root,n,r;if(!e)return i;if(!e.length)return i._root=w0(e),i;for(n=[{source:e,target:i._root=new Array(2)}];e=n.pop();)for(var s=0;s<2;++s)(r=e.source[s])&&(r.length?n.push({source:r,target:e.target[s]=new Array(2)}):e.target[s]=w0(r));return i};gn.add=l0;gn.addAll=u0;gn.cover=h0;gn.data=f0;gn.extent=d0;gn.find=p0;gn.remove=m0;gn.removeAll=g0;gn.root=_0;gn.size=v0;gn.visit=y0;gn.visitAfter=x0;gn.x=S0;function M0(i){let e=+this._x.call(null,i),n=+this._y.call(null,i);return E0(this.cover(e,n),e,n,i)}function E0(i,e,n,r){if(isNaN(e)||isNaN(n))return i;var s,o=i._root,a={data:r},l=i._x0,c=i._y0,u=i._x1,h=i._y1,d,f,p,g,v,_,m,w;if(!o)return i._root=a,i;for(;o.length;)if((v=e>=(d=(l+u)/2))?l=d:u=d,(_=n>=(f=(c+h)/2))?c=f:h=f,s=o,!(o=o[m=_<<1|v]))return s[m]=a,i;if(p=+i._x.call(null,o.data),g=+i._y.call(null,o.data),e===p&&n===g)return a.next=o,s?s[m]=a:i._root=a,i;do s=s?s[m]=new Array(4):i._root=new Array(4),(v=e>=(d=(l+u)/2))?l=d:u=d,(_=n>=(f=(c+h)/2))?c=f:h=f;while((m=_<<1|v)===(w=(g>=f)<<1|p>=d));return s[w]=o,s[m]=a,i}function A0(i){var e,n,r=i.length,s,o,a=new Array(r),l=new Array(r),c=1/0,u=1/0,h=-1/0,d=-1/0;for(n=0;n<r;++n)isNaN(s=+this._x.call(null,e=i[n]))||isNaN(o=+this._y.call(null,e))||(a[n]=s,l[n]=o,s<c&&(c=s),s>h&&(h=s),o<u&&(u=o),o>d&&(d=o));if(c>h||u>d)return this;for(this.cover(c,u).cover(h,d),n=0;n<r;++n)E0(this,a[n],l[n],i[n]);return this}function T0(i,e){if(isNaN(i=+i)||isNaN(e=+e))return this;var n=this._x0,r=this._y0,s=this._x1,o=this._y1;if(isNaN(n))s=(n=Math.floor(i))+1,o=(r=Math.floor(e))+1;else{for(var a=s-n||1,l=this._root,c,u;n>i||i>=s||r>e||e>=o;)switch(u=(e<r)<<1|i<n,c=new Array(4),c[u]=l,l=c,a*=2,u){case 0:s=n+a,o=r+a;break;case 1:n=s-a,o=r+a;break;case 2:s=n+a,r=o-a;break;case 3:n=s-a,r=o-a;break}this._root&&this._root.length&&(this._root=l)}return this._x0=n,this._y0=r,this._x1=s,this._y1=o,this}function C0(){var i=[];return this.visit(function(e){if(!e.length)do i.push(e.data);while(e=e.next)}),i}function R0(i){return arguments.length?this.cover(+i[0][0],+i[0][1]).cover(+i[1][0],+i[1][1]):isNaN(this._x0)?void 0:[[this._x0,this._y0],[this._x1,this._y1]]}function Bt(i,e,n,r,s){this.node=i,this.x0=e,this.y0=n,this.x1=r,this.y1=s}function P0(i,e,n){var r,s=this._x0,o=this._y0,a,l,c,u,h=this._x1,d=this._y1,f=[],p=this._root,g,v;for(p&&f.push(new Bt(p,s,o,h,d)),n==null?n=1/0:(s=i-n,o=e-n,h=i+n,d=e+n,n*=n);g=f.pop();)if(!(!(p=g.node)||(a=g.x0)>h||(l=g.y0)>d||(c=g.x1)<s||(u=g.y1)<o))if(p.length){var _=(a+c)/2,m=(l+u)/2;f.push(new Bt(p[3],_,m,c,u),new Bt(p[2],a,m,_,u),new Bt(p[1],_,l,c,m),new Bt(p[0],a,l,_,m)),(v=(e>=m)<<1|i>=_)&&(g=f[f.length-1],f[f.length-1]=f[f.length-1-v],f[f.length-1-v]=g)}else{var w=i-+this._x.call(null,p.data),T=e-+this._y.call(null,p.data),y=w*w+T*T;if(y<n){var b=Math.sqrt(n=y);s=i-b,o=e-b,h=i+b,d=e+b,r=p.data}}return r}function I0(i){if(isNaN(h=+this._x.call(null,i))||isNaN(d=+this._y.call(null,i)))return this;var e,n=this._root,r,s,o,a=this._x0,l=this._y0,c=this._x1,u=this._y1,h,d,f,p,g,v,_,m;if(!n)return this;if(n.length)for(;;){if((g=h>=(f=(a+c)/2))?a=f:c=f,(v=d>=(p=(l+u)/2))?l=p:u=p,e=n,!(n=n[_=v<<1|g]))return this;if(!n.length)break;(e[_+1&3]||e[_+2&3]||e[_+3&3])&&(r=e,m=_)}for(;n.data!==i;)if(s=n,!(n=n.next))return this;return(o=n.next)&&delete n.next,s?(o?s.next=o:delete s.next,this):e?(o?e[_]=o:delete e[_],(n=e[0]||e[1]||e[2]||e[3])&&n===(e[3]||e[2]||e[1]||e[0])&&!n.length&&(r?r[m]=n:this._root=n),this):(this._root=o,this)}function L0(i){for(var e=0,n=i.length;e<n;++e)this.remove(i[e]);return this}function D0(){return this._root}function O0(){var i=0;return this.visit(function(e){if(!e.length)do++i;while(e=e.next)}),i}function N0(i){var e=[],n,r=this._root,s,o,a,l,c;for(r&&e.push(new Bt(r,this._x0,this._y0,this._x1,this._y1));n=e.pop();)if(!i(r=n.node,o=n.x0,a=n.y0,l=n.x1,c=n.y1)&&r.length){var u=(o+l)/2,h=(a+c)/2;(s=r[3])&&e.push(new Bt(s,u,h,l,c)),(s=r[2])&&e.push(new Bt(s,o,h,u,c)),(s=r[1])&&e.push(new Bt(s,u,a,l,h)),(s=r[0])&&e.push(new Bt(s,o,a,u,h))}return this}function U0(i){var e=[],n=[],r;for(this._root&&e.push(new Bt(this._root,this._x0,this._y0,this._x1,this._y1));r=e.pop();){var s=r.node;if(s.length){var o,a=r.x0,l=r.y0,c=r.x1,u=r.y1,h=(a+c)/2,d=(l+u)/2;(o=s[0])&&e.push(new Bt(o,a,l,h,d)),(o=s[1])&&e.push(new Bt(o,h,l,c,d)),(o=s[2])&&e.push(new Bt(o,a,d,h,u)),(o=s[3])&&e.push(new Bt(o,h,d,c,u))}n.push(r)}for(;r=n.pop();)i(r.node,r.x0,r.y0,r.x1,r.y1);return this}function F0(i){return i[0]}function k0(i){return arguments.length?(this._x=i,this):this._x}function B0(i){return i[1]}function z0(i){return arguments.length?(this._y=i,this):this._y}function ia(i,e,n){var r=new gd(e??F0,n??B0,NaN,NaN,NaN,NaN);return i==null?r:r.addAll(i)}function gd(i,e,n,r,s,o){this._x=i,this._y=e,this._x0=n,this._y0=r,this._x1=s,this._y1=o,this._root=void 0}function V0(i){for(var e={data:i.data},n=e;i=i.next;)n=n.next={data:i.data};return e}var cn=ia.prototype=gd.prototype;cn.copy=function(){var i=new gd(this._x,this._y,this._x0,this._y0,this._x1,this._y1),e=this._root,n,r;if(!e)return i;if(!e.length)return i._root=V0(e),i;for(n=[{source:e,target:i._root=new Array(4)}];e=n.pop();)for(var s=0;s<4;++s)(r=e.source[s])&&(r.length?n.push({source:r,target:e.target[s]=new Array(4)}):e.target[s]=V0(r));return i};cn.add=M0;cn.addAll=A0;cn.cover=T0;cn.data=C0;cn.extent=R0;cn.find=P0;cn.remove=I0;cn.removeAll=L0;cn.root=D0;cn.size=O0;cn.visit=N0;cn.visitAfter=U0;cn.x=k0;cn.y=z0;function G0(i){let e=+this._x.call(null,i),n=+this._y.call(null,i),r=+this._z.call(null,i);return H0(this.cover(e,n,r),e,n,r,i)}function H0(i,e,n,r,s){if(isNaN(e)||isNaN(n)||isNaN(r))return i;var o,a=i._root,l={data:s},c=i._x0,u=i._y0,h=i._z0,d=i._x1,f=i._y1,p=i._z1,g,v,_,m,w,T,y,b,S,A,x;if(!a)return i._root=l,i;for(;a.length;)if((y=e>=(g=(c+d)/2))?c=g:d=g,(b=n>=(v=(u+f)/2))?u=v:f=v,(S=r>=(_=(h+p)/2))?h=_:p=_,o=a,!(a=a[A=S<<2|b<<1|y]))return o[A]=l,i;if(m=+i._x.call(null,a.data),w=+i._y.call(null,a.data),T=+i._z.call(null,a.data),e===m&&n===w&&r===T)return l.next=a,o?o[A]=l:i._root=l,i;do o=o?o[A]=new Array(8):i._root=new Array(8),(y=e>=(g=(c+d)/2))?c=g:d=g,(b=n>=(v=(u+f)/2))?u=v:f=v,(S=r>=(_=(h+p)/2))?h=_:p=_;while((A=S<<2|b<<1|y)===(x=(T>=_)<<2|(w>=v)<<1|m>=g));return o[x]=a,o[A]=l,i}function W0(i){Array.isArray(i)||(i=Array.from(i));let e=i.length,n=new Float64Array(e),r=new Float64Array(e),s=new Float64Array(e),o=1/0,a=1/0,l=1/0,c=-1/0,u=-1/0,h=-1/0;for(let d=0,f,p,g,v;d<e;++d)isNaN(p=+this._x.call(null,f=i[d]))||isNaN(g=+this._y.call(null,f))||isNaN(v=+this._z.call(null,f))||(n[d]=p,r[d]=g,s[d]=v,p<o&&(o=p),p>c&&(c=p),g<a&&(a=g),g>u&&(u=g),v<l&&(l=v),v>h&&(h=v));if(o>c||a>u||l>h)return this;this.cover(o,a,l).cover(c,u,h);for(let d=0;d<e;++d)H0(this,n[d],r[d],s[d],i[d]);return this}function X0(i,e,n){if(isNaN(i=+i)||isNaN(e=+e)||isNaN(n=+n))return this;var r=this._x0,s=this._y0,o=this._z0,a=this._x1,l=this._y1,c=this._z1;if(isNaN(r))a=(r=Math.floor(i))+1,l=(s=Math.floor(e))+1,c=(o=Math.floor(n))+1;else{for(var u=a-r||1,h=this._root,d,f;r>i||i>=a||s>e||e>=l||o>n||n>=c;)switch(f=(n<o)<<2|(e<s)<<1|i<r,d=new Array(8),d[f]=h,h=d,u*=2,f){case 0:a=r+u,l=s+u,c=o+u;break;case 1:r=a-u,l=s+u,c=o+u;break;case 2:a=r+u,s=l-u,c=o+u;break;case 3:r=a-u,s=l-u,c=o+u;break;case 4:a=r+u,l=s+u,o=c-u;break;case 5:r=a-u,l=s+u,o=c-u;break;case 6:a=r+u,s=l-u,o=c-u;break;case 7:r=a-u,s=l-u,o=c-u;break}this._root&&this._root.length&&(this._root=h)}return this._x0=r,this._y0=s,this._z0=o,this._x1=a,this._y1=l,this._z1=c,this}function q0(){var i=[];return this.visit(function(e){if(!e.length)do i.push(e.data);while(e=e.next)}),i}function j0(i){return arguments.length?this.cover(+i[0][0],+i[0][1],+i[0][2]).cover(+i[1][0],+i[1][1],+i[1][2]):isNaN(this._x0)?void 0:[[this._x0,this._y0,this._z0],[this._x1,this._y1,this._z1]]}function ot(i,e,n,r,s,o,a){this.node=i,this.x0=e,this.y0=n,this.z0=r,this.x1=s,this.y1=o,this.z1=a}function Y0(i,e,n,r){var s,o=this._x0,a=this._y0,l=this._z0,c,u,h,d,f,p,g=this._x1,v=this._y1,_=this._z1,m=[],w=this._root,T,y;for(w&&m.push(new ot(w,o,a,l,g,v,_)),r==null?r=1/0:(o=i-r,a=e-r,l=n-r,g=i+r,v=e+r,_=n+r,r*=r);T=m.pop();)if(!(!(w=T.node)||(c=T.x0)>g||(u=T.y0)>v||(h=T.z0)>_||(d=T.x1)<o||(f=T.y1)<a||(p=T.z1)<l))if(w.length){var b=(c+d)/2,S=(u+f)/2,A=(h+p)/2;m.push(new ot(w[7],b,S,A,d,f,p),new ot(w[6],c,S,A,b,f,p),new ot(w[5],b,u,A,d,S,p),new ot(w[4],c,u,A,b,S,p),new ot(w[3],b,S,h,d,f,A),new ot(w[2],c,S,h,b,f,A),new ot(w[1],b,u,h,d,S,A),new ot(w[0],c,u,h,b,S,A)),(y=(n>=A)<<2|(e>=S)<<1|i>=b)&&(T=m[m.length-1],m[m.length-1]=m[m.length-1-y],m[m.length-1-y]=T)}else{var x=i-+this._x.call(null,w.data),C=e-+this._y.call(null,w.data),L=n-+this._z.call(null,w.data),P=x*x+C*C+L*L;if(P<r){var O=Math.sqrt(r=P);o=i-O,a=e-O,l=n-O,g=i+O,v=e+O,_=n+O,s=w.data}}return s}var sT=(i,e,n,r,s,o)=>Math.sqrt((i-r)**2+(e-s)**2+(n-o)**2);function $0(i,e,n,r){let s=[],o=i-r,a=e-r,l=n-r,c=i+r,u=e+r,h=n+r;return this.visit((d,f,p,g,v,_,m)=>{if(!d.length)do{let w=d.data;sT(i,e,n,this._x(w),this._y(w),this._z(w))<=r&&s.push(w)}while(d=d.next);return f>c||p>u||g>h||v<o||_<a||m<l}),s}function Z0(i){if(isNaN(f=+this._x.call(null,i))||isNaN(p=+this._y.call(null,i))||isNaN(g=+this._z.call(null,i)))return this;var e,n=this._root,r,s,o,a=this._x0,l=this._y0,c=this._z0,u=this._x1,h=this._y1,d=this._z1,f,p,g,v,_,m,w,T,y,b,S;if(!n)return this;if(n.length)for(;;){if((w=f>=(v=(a+u)/2))?a=v:u=v,(T=p>=(_=(l+h)/2))?l=_:h=_,(y=g>=(m=(c+d)/2))?c=m:d=m,e=n,!(n=n[b=y<<2|T<<1|w]))return this;if(!n.length)break;(e[b+1&7]||e[b+2&7]||e[b+3&7]||e[b+4&7]||e[b+5&7]||e[b+6&7]||e[b+7&7])&&(r=e,S=b)}for(;n.data!==i;)if(s=n,!(n=n.next))return this;return(o=n.next)&&delete n.next,s?(o?s.next=o:delete s.next,this):e?(o?e[b]=o:delete e[b],(n=e[0]||e[1]||e[2]||e[3]||e[4]||e[5]||e[6]||e[7])&&n===(e[7]||e[6]||e[5]||e[4]||e[3]||e[2]||e[1]||e[0])&&!n.length&&(r?r[S]=n:this._root=n),this):(this._root=o,this)}function K0(i){for(var e=0,n=i.length;e<n;++e)this.remove(i[e]);return this}function J0(){return this._root}function Q0(){var i=0;return this.visit(function(e){if(!e.length)do++i;while(e=e.next)}),i}function e_(i){var e=[],n,r=this._root,s,o,a,l,c,u,h;for(r&&e.push(new ot(r,this._x0,this._y0,this._z0,this._x1,this._y1,this._z1));n=e.pop();)if(!i(r=n.node,o=n.x0,a=n.y0,l=n.z0,c=n.x1,u=n.y1,h=n.z1)&&r.length){var d=(o+c)/2,f=(a+u)/2,p=(l+h)/2;(s=r[7])&&e.push(new ot(s,d,f,p,c,u,h)),(s=r[6])&&e.push(new ot(s,o,f,p,d,u,h)),(s=r[5])&&e.push(new ot(s,d,a,p,c,f,h)),(s=r[4])&&e.push(new ot(s,o,a,p,d,f,h)),(s=r[3])&&e.push(new ot(s,d,f,l,c,u,p)),(s=r[2])&&e.push(new ot(s,o,f,l,d,u,p)),(s=r[1])&&e.push(new ot(s,d,a,l,c,f,p)),(s=r[0])&&e.push(new ot(s,o,a,l,d,f,p))}return this}function t_(i){var e=[],n=[],r;for(this._root&&e.push(new ot(this._root,this._x0,this._y0,this._z0,this._x1,this._y1,this._z1));r=e.pop();){var s=r.node;if(s.length){var o,a=r.x0,l=r.y0,c=r.z0,u=r.x1,h=r.y1,d=r.z1,f=(a+u)/2,p=(l+h)/2,g=(c+d)/2;(o=s[0])&&e.push(new ot(o,a,l,c,f,p,g)),(o=s[1])&&e.push(new ot(o,f,l,c,u,p,g)),(o=s[2])&&e.push(new ot(o,a,p,c,f,h,g)),(o=s[3])&&e.push(new ot(o,f,p,c,u,h,g)),(o=s[4])&&e.push(new ot(o,a,l,g,f,p,d)),(o=s[5])&&e.push(new ot(o,f,l,g,u,p,d)),(o=s[6])&&e.push(new ot(o,a,p,g,f,h,d)),(o=s[7])&&e.push(new ot(o,f,p,g,u,h,d))}n.push(r)}for(;r=n.pop();)i(r.node,r.x0,r.y0,r.z0,r.x1,r.y1,r.z1);return this}function n_(i){return i[0]}function i_(i){return arguments.length?(this._x=i,this):this._x}function r_(i){return i[1]}function s_(i){return arguments.length?(this._y=i,this):this._y}function o_(i){return i[2]}function a_(i){return arguments.length?(this._z=i,this):this._z}function ra(i,e,n,r){var s=new _d(e??n_,n??r_,r??o_,NaN,NaN,NaN,NaN,NaN,NaN);return i==null?s:s.addAll(i)}function _d(i,e,n,r,s,o,a,l,c){this._x=i,this._y=e,this._z=n,this._x0=r,this._y0=s,this._z0=o,this._x1=a,this._y1=l,this._z1=c,this._root=void 0}function l_(i){for(var e={data:i.data},n=e;i=i.next;)n=n.next={data:i.data};return e}var Ht=ra.prototype=_d.prototype;Ht.copy=function(){var i=new _d(this._x,this._y,this._z,this._x0,this._y0,this._z0,this._x1,this._y1,this._z1),e=this._root,n,r;if(!e)return i;if(!e.length)return i._root=l_(e),i;for(n=[{source:e,target:i._root=new Array(8)}];e=n.pop();)for(var s=0;s<8;++s)(r=e.source[s])&&(r.length?n.push({source:r,target:e.target[s]=new Array(8)}):e.target[s]=l_(r));return i};Ht.add=G0;Ht.addAll=W0;Ht.cover=X0;Ht.data=q0;Ht.extent=j0;Ht.find=Y0;Ht.findAllWithinRadius=$0;Ht.remove=Z0;Ht.removeAll=K0;Ht.root=J0;Ht.size=Q0;Ht.visit=e_;Ht.visitAfter=t_;Ht.x=i_;Ht.y=s_;Ht.z=a_;function Fn(i){return function(){return i}}function Qn(i){return(i()-.5)*1e-6}function oT(i){return i.index}function c_(i,e){var n=i.get(e);if(!n)throw new Error("node not found: "+e);return n}function sa(i){var e=oT,n=f,r,s=Fn(30),o,a,l,c,u,h,d=1;i==null&&(i=[]);function f(m){return 1/Math.min(c[m.source.index],c[m.target.index])}function p(m){for(var w=0,T=i.length;w<d;++w)for(var y=0,b,S,A,x=0,C=0,L=0,P,O;y<T;++y)b=i[y],S=b.source,A=b.target,x=A.x+A.vx-S.x-S.vx||Qn(h),l>1&&(C=A.y+A.vy-S.y-S.vy||Qn(h)),l>2&&(L=A.z+A.vz-S.z-S.vz||Qn(h)),P=Math.sqrt(x*x+C*C+L*L),P=(P-o[y])/P*m*r[y],x*=P,C*=P,L*=P,A.vx-=x*(O=u[y]),l>1&&(A.vy-=C*O),l>2&&(A.vz-=L*O),S.vx+=x*(O=1-O),l>1&&(S.vy+=C*O),l>2&&(S.vz+=L*O)}function g(){if(a){var m,w=a.length,T=i.length,y=new Map(a.map((S,A)=>[e(S,A,a),S])),b;for(m=0,c=new Array(w);m<T;++m)b=i[m],b.index=m,typeof b.source!="object"&&(b.source=c_(y,b.source)),typeof b.target!="object"&&(b.target=c_(y,b.target)),c[b.source.index]=(c[b.source.index]||0)+1,c[b.target.index]=(c[b.target.index]||0)+1;for(m=0,u=new Array(T);m<T;++m)b=i[m],u[m]=c[b.source.index]/(c[b.source.index]+c[b.target.index]);r=new Array(T),v(),o=new Array(T),_()}}function v(){if(a)for(var m=0,w=i.length;m<w;++m)r[m]=+n(i[m],m,i)}function _(){if(a)for(var m=0,w=i.length;m<w;++m)o[m]=+s(i[m],m,i)}return p.initialize=function(m,...w){a=m,h=w.find(T=>typeof T=="function")||Math.random,l=w.find(T=>[1,2,3].includes(T))||2,g()},p.links=function(m){return arguments.length?(i=m,g(),p):i},p.id=function(m){return arguments.length?(e=m,p):e},p.iterations=function(m){return arguments.length?(d=+m,p):d},p.strength=function(m){return arguments.length?(n=typeof m=="function"?m:Fn(+m),v(),p):n},p.distance=function(m){return arguments.length?(s=typeof m=="function"?m:Fn(+m),_(),p):s},p}var aT={value:()=>{}};function h_(){for(var i=0,e=arguments.length,n={},r;i<e;++i){if(!(r=arguments[i]+"")||r in n||/[\s.]/.test(r))throw new Error("illegal type: "+r);n[r]=[]}return new hu(n)}function hu(i){this._=i}function lT(i,e){return i.trim().split(/^|\s+/).map(function(n){var r="",s=n.indexOf(".");if(s>=0&&(r=n.slice(s+1),n=n.slice(0,s)),n&&!e.hasOwnProperty(n))throw new Error("unknown type: "+n);return{type:n,name:r}})}hu.prototype=h_.prototype={constructor:hu,on:function(i,e){var n=this._,r=lT(i+"",n),s,o=-1,a=r.length;if(arguments.length<2){for(;++o<a;)if((s=(i=r[o]).type)&&(s=cT(n[s],i.name)))return s;return}if(e!=null&&typeof e!="function")throw new Error("invalid callback: "+e);for(;++o<a;)if(s=(i=r[o]).type)n[s]=u_(n[s],i.name,e);else if(e==null)for(s in n)n[s]=u_(n[s],i.name,null);return this},copy:function(){var i={},e=this._;for(var n in e)i[n]=e[n].slice();return new hu(i)},call:function(i,e){if((s=arguments.length-2)>0)for(var n=new Array(s),r=0,s,o;r<s;++r)n[r]=arguments[r+2];if(!this._.hasOwnProperty(i))throw new Error("unknown type: "+i);for(o=this._[i],r=0,s=o.length;r<s;++r)o[r].value.apply(e,n)},apply:function(i,e,n){if(!this._.hasOwnProperty(i))throw new Error("unknown type: "+i);for(var r=this._[i],s=0,o=r.length;s<o;++s)r[s].value.apply(e,n)}};function cT(i,e){for(var n=0,r=i.length,s;n<r;++n)if((s=i[n]).name===e)return s.value}function u_(i,e,n){for(var r=0,s=i.length;r<s;++r)if(i[r].name===e){i[r]=aT,i=i.slice(0,r).concat(i.slice(r+1));break}return n!=null&&i.push({name:e,value:n}),i}var Di=h_;var Ys=0,aa=0,oa=0,d_=1e3,fu,la,du=0,zr=0,pu=0,ca=typeof performance=="object"&&performance.now?performance:Date,p_=typeof window=="object"&&window.requestAnimationFrame?window.requestAnimationFrame.bind(window):function(i){setTimeout(i,17)};function ha(){return zr||(p_(uT),zr=ca.now()+pu)}function uT(){zr=0}function ua(){this._call=this._time=this._next=null}ua.prototype=$s.prototype={constructor:ua,restart:function(i,e,n){if(typeof i!="function")throw new TypeError("callback is not a function");n=(n==null?ha():+n)+(e==null?0:+e),!this._next&&la!==this&&(la?la._next=this:fu=this,la=this),this._call=i,this._time=n,vd()},stop:function(){this._call&&(this._call=null,this._time=1/0,vd())}};function $s(i,e,n){var r=new ua;return r.restart(i,e,n),r}function m_(){ha(),++Ys;for(var i=fu,e;i;)(e=zr-i._time)>=0&&i._call.call(void 0,e),i=i._next;--Ys}function f_(){zr=(du=ca.now())+pu,Ys=aa=0;try{m_()}finally{Ys=0,fT(),zr=0}}function hT(){var i=ca.now(),e=i-du;e>d_&&(pu-=e,du=i)}function fT(){for(var i,e=fu,n,r=1/0;e;)e._call?(r>e._time&&(r=e._time),i=e,e=e._next):(n=e._next,e._next=null,e=i?i._next=n:fu=n);la=i,vd(r)}function vd(i){if(!Ys){aa&&(aa=clearTimeout(aa));var e=i-zr;e>24?(i<1/0&&(aa=setTimeout(f_,i-ca.now()-pu)),oa&&(oa=clearInterval(oa))):(oa||(du=ca.now(),oa=setInterval(hT,d_)),Ys=1,p_(f_))}}function mu(i,e,n){var r=new ua;return e=e==null?0:+e,r.restart(s=>{r.stop(),i(s+e)},e,n),r}function g_(){let i=1;return()=>(i=(1664525*i+1013904223)%4294967296)/4294967296}var __=3;function gu(i){return i.x}function yd(i){return i.y}function v_(i){return i.z}var dT=10,pT=Math.PI*(3-Math.sqrt(5)),mT=Math.PI*20/(9+Math.sqrt(221));function fa(i,e){e=e||2;var n=Math.min(__,Math.max(1,Math.round(e))),r,s=1,o=.001,a=1-Math.pow(o,1/300),l=0,c=.6,u=new Map,h=$s(p),d=Di("tick","end"),f=g_();i==null&&(i=[]);function p(){g(),d.call("tick",r),s<o&&(h.stop(),d.call("end",r))}function g(m){var w,T=i.length,y;m===void 0&&(m=1);for(var b=0;b<m;++b)for(s+=(l-s)*a,u.forEach(function(S){S(s)}),w=0;w<T;++w)y=i[w],y.fx==null?y.x+=y.vx*=c:(y.x=y.fx,y.vx=0),n>1&&(y.fy==null?y.y+=y.vy*=c:(y.y=y.fy,y.vy=0)),n>2&&(y.fz==null?y.z+=y.vz*=c:(y.z=y.fz,y.vz=0));return r}function v(){for(var m=0,w=i.length,T;m<w;++m){if(T=i[m],T.index=m,T.fx!=null&&(T.x=T.fx),T.fy!=null&&(T.y=T.fy),T.fz!=null&&(T.z=T.fz),isNaN(T.x)||n>1&&isNaN(T.y)||n>2&&isNaN(T.z)){var y=dT*(n>2?Math.cbrt(.5+m):n>1?Math.sqrt(.5+m):m),b=m*pT,S=m*mT;n===1?T.x=y:n===2?(T.x=y*Math.cos(b),T.y=y*Math.sin(b)):(T.x=y*Math.sin(b)*Math.cos(S),T.y=y*Math.cos(b),T.z=y*Math.sin(b)*Math.sin(S))}(isNaN(T.vx)||n>1&&isNaN(T.vy)||n>2&&isNaN(T.vz))&&(T.vx=0,n>1&&(T.vy=0),n>2&&(T.vz=0))}}function _(m){return m.initialize&&m.initialize(i,f,n),m}return v(),r={tick:g,restart:function(){return h.restart(p),r},stop:function(){return h.stop(),r},numDimensions:function(m){return arguments.length?(n=Math.min(__,Math.max(1,Math.round(m))),u.forEach(_),r):n},nodes:function(m){return arguments.length?(i=m,v(),u.forEach(_),r):i},alpha:function(m){return arguments.length?(s=+m,r):s},alphaMin:function(m){return arguments.length?(o=+m,r):o},alphaDecay:function(m){return arguments.length?(a=+m,r):+a},alphaTarget:function(m){return arguments.length?(l=+m,r):l},velocityDecay:function(m){return arguments.length?(c=1-m,r):1-c},randomSource:function(m){return arguments.length?(f=m,u.forEach(_),r):f},force:function(m,w){return arguments.length>1?(w==null?u.delete(m):u.set(m,_(w)),r):u.get(m)},find:function(){var m=Array.prototype.slice.call(arguments),w=m.shift()||0,T=(n>1?m.shift():null)||0,y=(n>2?m.shift():null)||0,b=m.shift()||1/0,S=0,A=i.length,x,C,L,P,O,U;for(b*=b,S=0;S<A;++S)O=i[S],x=w-O.x,C=T-(O.y||0),L=y-(O.z||0),P=x*x+C*C+L*L,P<b&&(U=O,b=P);return U},on:function(m,w){return arguments.length>1?(d.on(m,w),r):d.on(m)}}}function da(){var i,e,n,r,s,o=Fn(-30),a,l=1,c=1/0,u=.81;function h(g){var v,_=i.length,m=(e===1?na(i,gu):e===2?ia(i,gu,yd):e===3?ra(i,gu,yd,v_):null).visitAfter(f);for(s=g,v=0;v<_;++v)n=i[v],m.visit(p)}function d(){if(i){var g,v=i.length,_;for(a=new Array(v),g=0;g<v;++g)_=i[g],a[_.index]=+o(_,g,i)}}function f(g){var v=0,_,m,w=0,T,y,b,S,A=g.length;if(A){for(T=y=b=S=0;S<A;++S)(_=g[S])&&(m=Math.abs(_.value))&&(v+=_.value,w+=m,T+=m*(_.x||0),y+=m*(_.y||0),b+=m*(_.z||0));v*=Math.sqrt(4/A),g.x=T/w,e>1&&(g.y=y/w),e>2&&(g.z=b/w)}else{_=g,_.x=_.data.x,e>1&&(_.y=_.data.y),e>2&&(_.z=_.data.z);do v+=a[_.data.index];while(_=_.next)}g.value=v}function p(g,v,_,m,w){if(!g.value)return!0;var T=[_,m,w][e-1],y=g.x-n.x,b=e>1?g.y-n.y:0,S=e>2?g.z-n.z:0,A=T-v,x=y*y+b*b+S*S;if(A*A/u<x)return x<c&&(y===0&&(y=Qn(r),x+=y*y),e>1&&b===0&&(b=Qn(r),x+=b*b),e>2&&S===0&&(S=Qn(r),x+=S*S),x<l&&(x=Math.sqrt(l*x)),n.vx+=y*g.value*s/x,e>1&&(n.vy+=b*g.value*s/x),e>2&&(n.vz+=S*g.value*s/x)),!0;if(g.length||x>=c)return;(g.data!==n||g.next)&&(y===0&&(y=Qn(r),x+=y*y),e>1&&b===0&&(b=Qn(r),x+=b*b),e>2&&S===0&&(S=Qn(r),x+=S*S),x<l&&(x=Math.sqrt(l*x)));do g.data!==n&&(A=a[g.data.index]*s/x,n.vx+=y*A,e>1&&(n.vy+=b*A),e>2&&(n.vz+=S*A));while(g=g.next)}return h.initialize=function(g,...v){i=g,r=v.find(_=>typeof _=="function")||Math.random,e=v.find(_=>[1,2,3].includes(_))||2,d()},h.strength=function(g){return arguments.length?(o=typeof g=="function"?g:Fn(+g),d(),h):o},h.distanceMin=function(g){return arguments.length?(l=g*g,h):Math.sqrt(l)},h.distanceMax=function(g){return arguments.length?(c=g*g,h):Math.sqrt(c)},h.theta=function(g){return arguments.length?(u=g*g,h):Math.sqrt(u)},h}function pa(i,e,n,r){var s,o,a=Fn(.1),l,c;typeof i!="function"&&(i=Fn(+i)),e==null&&(e=0),n==null&&(n=0),r==null&&(r=0);function u(d){for(var f=0,p=s.length;f<p;++f){var g=s[f],v=g.x-e||1e-6,_=(g.y||0)-n||1e-6,m=(g.z||0)-r||1e-6,w=Math.sqrt(v*v+_*_+m*m),T=(c[f]-w)*l[f]*d/w;g.vx+=v*T,o>1&&(g.vy+=_*T),o>2&&(g.vz+=m*T)}}function h(){if(s){var d,f=s.length;for(l=new Array(f),c=new Array(f),d=0;d<f;++d)c[d]=+i(s[d],d,s),l[d]=isNaN(c[d])?0:+a(s[d],d,s)}}return u.initialize=function(d,...f){s=d,o=f.find(p=>[1,2,3].includes(p))||2,h()},u.strength=function(d){return arguments.length?(a=typeof d=="function"?d:Fn(+d),h(),u):a},u.radius=function(d){return arguments.length?(i=typeof d=="function"?d:Fn(+d),h(),u):i},u.x=function(d){return arguments.length?(e=+d,u):e},u.y=function(d){return arguments.length?(n=+d,u):n},u.z=function(d){return arguments.length?(r=+d,u):r},u}function xd(i){_T(i);let e=gT(i);return i.on=e.on,i.off=e.off,i.fire=e.fire,i}function gT(i){let e=Object.create(null);return{on:function(n,r,s){if(typeof r!="function")throw new Error("callback is expected to be a function");let o=e[n];return o||(o=e[n]=[]),o.push({callback:r,ctx:s}),i},off:function(n,r){if(typeof n>"u")return e=Object.create(null),i;if(e[n])if(typeof r!="function")delete e[n];else{let a=e[n];for(let l=0;l<a.length;++l)a[l].callback===r&&a.splice(l,1)}return i},fire:function(n){let r=e[n];if(!r)return i;let s;arguments.length>1&&(s=Array.prototype.slice.call(arguments,1));for(let o=0;o<r.length;++o){let a=r[o];a.callback.apply(a.ctx,s)}return i}}}function _T(i){if(!i)throw new Error("Eventify cannot use falsy object as events subject");let e=["on","fire","off"];for(let n=0;n<e.length;++n)if(i.hasOwnProperty(e[n]))throw new Error("Subject cannot be eventified, since it already has property '"+e[n]+"'")}var b_=vT;function vT(i){if(i=i||{},"uniqueLinkId"in i&&(console.warn("ngraph.graph: Starting from version 0.14 `uniqueLinkId` is deprecated.\nUse `multigraph` option instead\n",`
`,`Note: there is also change in default behavior: From now on each graph
is considered to be not a multigraph by default (each edge is unique).`),i.multigraph=i.uniqueLinkId),i.multigraph===void 0&&(i.multigraph=!1),typeof Map!="function")throw new Error("ngraph.graph requires `Map` to be defined. Please polyfill it before using ngraph");var e=new Map,n=new Map,r={},s=0,o=i.multigraph?y:T,a=[],l=k,c=k,u=k,h=k,d={version:20,addNode:v,addLink:w,removeLink:x,removeNode:m,getNode:_,getNodeCount:b,getLinkCount:S,getEdgeCount:S,getLinksCount:S,getNodesCount:b,getLinks:A,forEachNode:j,forEachLinkedNode:M,forEachLink:U,beginUpdate:u,endUpdate:h,clear:O,hasLink:L,hasNode:_,getLink:L,getLinkById:P};return xd(d),f(),d;function f(){var B=d.on;d.on=Q;function Q(){return d.beginUpdate=u=q,d.endUpdate=h=W,l=p,c=g,d.on=B,B.apply(d,arguments)}}function p(B,Q){a.push({link:B,changeType:Q})}function g(B,Q){a.push({node:B,changeType:Q})}function v(B,Q){if(B===void 0)throw new Error("Invalid node identifier");u();var re=_(B);return re?(re.data=Q,c(re,"update")):(re=new yT(B,Q),c(re,"add")),e.set(B,re),h(),re}function _(B){return e.get(B)}function m(B){var Q=_(B);if(!Q)return!1;u();var re=Q.links;return re&&(re.forEach(C),Q.links=null),e.delete(B),c(Q,"remove"),h(),!0}function w(B,Q,re){u();var be=_(B)||v(B),ee=_(Q)||v(Q),oe=o(B,Q,re),V=n.has(oe.id);return n.set(oe.id,oe),y_(be,oe),B!==Q&&y_(ee,oe),l(oe,V?"update":"add"),h(),oe}function T(B,Q,re){var be=_u(B,Q),ee=n.get(be);return ee?(ee.data=re,ee):new x_(B,Q,re,be)}function y(B,Q,re){var be=_u(B,Q),ee=r.hasOwnProperty(be);if(ee||L(B,Q)){ee||(r[be]=0);var oe="@"+ ++r[be];be=_u(B+oe,Q+oe)}return new x_(B,Q,re,be)}function b(){return e.size}function S(){return n.size}function A(B){var Q=_(B);return Q?Q.links:null}function x(B,Q){return Q!==void 0&&(B=L(B,Q)),C(B)}function C(B){if(!B||!n.get(B.id))return!1;u(),n.delete(B.id);var Q=_(B.fromId),re=_(B.toId);return Q&&Q.links.delete(B),re&&re.links.delete(B),l(B,"remove"),h(),!0}function L(B,Q){if(!(B===void 0||Q===void 0))return n.get(_u(B,Q))}function P(B){if(B!==void 0)return n.get(B)}function O(){u(),j(function(B){m(B.id)}),h()}function U(B){if(typeof B=="function")for(var Q=n.values(),re=Q.next();!re.done;){if(B(re.value))return!0;re=Q.next()}}function M(B,Q,re){var be=_(B);if(be&&be.links&&typeof Q=="function")return re?D(be.links,B,Q):I(be.links,B,Q)}function I(B,Q,re){for(var be,ee=B.values(),oe=ee.next();!oe.done;){var V=oe.value,Z=V.fromId===Q?V.toId:V.fromId;if(be=re(e.get(Z),V),be)return!0;oe=ee.next()}}function D(B,Q,re){for(var be,ee=B.values(),oe=ee.next();!oe.done;){var V=oe.value;if(V.fromId===Q&&(be=re(e.get(V.toId),V),be))return!0;oe=ee.next()}}function k(){}function q(){s+=1}function W(){s-=1,s===0&&a.length>0&&(d.fire("changed",a),a.length=0)}function j(B){if(typeof B!="function")throw new Error("Function is expected to iterate over graph nodes. You passed "+B);for(var Q=e.values(),re=Q.next();!re.done;){if(B(re.value))return!0;re=Q.next()}}}function yT(i,e){this.id=i,this.links=null,this.data=e}function y_(i,e){i.links?i.links.add(e):i.links=new Set([e])}function x_(i,e,n,r){this.fromId=i,this.toId=e,this.data=n,this.id=r}function _u(i,e){return i.toString()+"\u{1F449} "+e.toString()}var Jv=oS(ev(),1);function KT(i){var e=typeof i;return i!=null&&(e=="object"||e=="function")}var Hr=KT;var JT=typeof global=="object"&&global&&global.Object===Object&&global,tv=JT;var QT=typeof self=="object"&&self&&self.Object===Object&&self,eC=tv||QT||Function("return this")(),yu=eC;var tC=function(){return yu.Date.now()},xu=tC;var nC=/\s/;function iC(i){for(var e=i.length;e--&&nC.test(i.charAt(e)););return e}var nv=iC;var rC=/^\s+/;function sC(i){return i&&i.slice(0,nv(i)+1).replace(rC,"")}var iv=sC;var oC=yu.Symbol,Zs=oC;var rv=Object.prototype,aC=rv.hasOwnProperty,lC=rv.toString,ga=Zs?Zs.toStringTag:void 0;function cC(i){var e=aC.call(i,ga),n=i[ga];try{i[ga]=void 0;var r=!0}catch{}var s=lC.call(i);return r&&(e?i[ga]=n:delete i[ga]),s}var sv=cC;var uC=Object.prototype,hC=uC.toString;function fC(i){return hC.call(i)}var ov=fC;var dC="[object Null]",pC="[object Undefined]",av=Zs?Zs.toStringTag:void 0;function mC(i){return i==null?i===void 0?pC:dC:av&&av in Object(i)?sv(i):ov(i)}var lv=mC;function gC(i){return i!=null&&typeof i=="object"}var cv=gC;var _C="[object Symbol]";function vC(i){return typeof i=="symbol"||cv(i)&&lv(i)==_C}var uv=vC;var hv=NaN,yC=/^[-+]0x[0-9a-f]+$/i,xC=/^0b[01]+$/i,bC=/^0o[0-7]+$/i,SC=parseInt;function wC(i){if(typeof i=="number")return i;if(uv(i))return hv;if(Hr(i)){var e=typeof i.valueOf=="function"?i.valueOf():i;i=Hr(e)?e+"":e}if(typeof i!="string")return i===0?i:+i;i=iv(i);var n=xC.test(i);return n||bC.test(i)?SC(i.slice(2),n?2:8):yC.test(i)?hv:+i}var Ld=wC;var MC="Expected a function",EC=Math.max,AC=Math.min;function TC(i,e,n){var r,s,o,a,l,c,u=0,h=!1,d=!1,f=!0;if(typeof i!="function")throw new TypeError(MC);e=Ld(e)||0,Hr(n)&&(h=!!n.leading,d="maxWait"in n,o=d?EC(Ld(n.maxWait)||0,e):o,f="trailing"in n?!!n.trailing:f);function p(S){var A=r,x=s;return r=s=void 0,u=S,a=i.apply(x,A),a}function g(S){return u=S,l=setTimeout(m,e),h?p(S):a}function v(S){var A=S-c,x=S-u,C=e-A;return d?AC(C,o-x):C}function _(S){var A=S-c,x=S-u;return c===void 0||A>=e||A<0||d&&x>=o}function m(){var S=xu();if(_(S))return w(S);l=setTimeout(m,v(S))}function w(S){return l=void 0,f&&r?p(S):(r=s=void 0,a)}function T(){l!==void 0&&clearTimeout(l),u=0,r=c=s=l=void 0}function y(){return l===void 0?a:w(xu())}function b(){var S=xu(),A=_(S);if(r=arguments,s=this,c=S,A){if(l===void 0)return g(c);if(d)return clearTimeout(l),l=setTimeout(m,e),p(c)}return l===void 0&&(l=setTimeout(m,e)),a}return b.cancel=T,b.flush=y,b}var bu=TC;function fv(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function CC(i){if(Array.isArray(i))return i}function RC(i,e){if(!(i instanceof e))throw new TypeError("Cannot call a class as a function")}function PC(i,e,n){return Object.defineProperty(i,"prototype",{writable:!1}),i}function IC(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function LC(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function DC(i,e){return CC(i)||IC(i,e)||OC(i,e)||LC()}function OC(i,e){if(i){if(typeof i=="string")return fv(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?fv(i,e):void 0}}var NC=PC(function i(e,n){var r=n.default,s=r===void 0?null:r,o=n.triggerUpdate,a=o===void 0?!0:o,l=n.onChange,c=l===void 0?function(u,h){}:l;RC(this,i),this.name=e,this.defaultVal=s,this.triggerUpdate=a,this.onChange=c});function ei(i){var e=i.stateInit,n=e===void 0?function(){return{}}:e,r=i.props,s=r===void 0?{}:r,o=i.methods,a=o===void 0?{}:o,l=i.aliases,c=l===void 0?{}:l,u=i.init,h=u===void 0?function(){}:u,d=i.update,f=d===void 0?function(){}:d,p=Object.keys(s).map(function(g){return new NC(g,s[g])});return function g(){for(var v=arguments.length,_=new Array(v),m=0;m<v;m++)_[m]=arguments[m];var w=!!(this instanceof g&&this.constructor),T=w?_.shift():void 0,y=_[0],b=y===void 0?{}:y,S=Object.assign({},n instanceof Function?n(b):n,{initialised:!1}),A={};function x(P){return C(P,b),L(),x}var C=function(O,U){h.call(x,O,S,U),S.initialised=!0},L=bu(function(){S.initialised&&(f.call(x,S,A),A={})},1);return p.forEach(function(P){x[P.name]=O(P);function O(U){var M=U.name,I=U.triggerUpdate,D=I===void 0?!1:I,k=U.onChange,q=k===void 0?function(B,Q){}:k,W=U.defaultVal,j=W===void 0?null:W;return function(B){var Q=S[M];if(!arguments.length)return Q;var re=B===void 0?j:B;return S[M]=re,q.call(x,re,S,Q),!A.hasOwnProperty(M)&&(A[M]=Q),D&&L(),x}}}),Object.keys(a).forEach(function(P){x[P]=function(){for(var O,U=arguments.length,M=new Array(U),I=0;I<U;I++)M[I]=arguments[I];return(O=a[P]).call.apply(O,[x,S].concat(M))}}),Object.entries(c).forEach(function(P){var O=DC(P,2),U=O[0],M=O[1];return x[U]=x[M]}),x.resetProps=function(){return p.forEach(function(P){x[P.name](P.defaultVal)}),x},x.resetProps(),S._rerender=L,w&&T&&x(T),x}}var we=(function(i){return typeof i=="function"?i:typeof i=="string"?function(e){return e[i]}:function(e){return i}});var Ks=class extends Map{constructor(e,n=kC){if(super(),Object.defineProperties(this,{_intern:{value:new Map},_key:{value:n}}),e!=null)for(let[r,s]of e)this.set(r,s)}get(e){return super.get(dv(this,e))}has(e){return super.has(dv(this,e))}set(e,n){return super.set(UC(this,e),n)}delete(e){return super.delete(FC(this,e))}};function dv({_intern:i,_key:e},n){let r=e(n);return i.has(r)?i.get(r):n}function UC({_intern:i,_key:e},n){let r=e(n);return i.has(r)?i.get(r):(i.set(r,n),n)}function FC({_intern:i,_key:e},n){let r=e(n);return i.has(r)&&(n=i.get(r),i.delete(r)),n}function kC(i){return i!==null&&typeof i=="object"?i.valueOf():i}function Wr(i,e){let n;if(e===void 0)for(let r of i)r!=null&&(n<r||n===void 0&&r>=r)&&(n=r);else{let r=-1;for(let s of i)(s=e(s,++r,i))!=null&&(n<s||n===void 0&&s>=s)&&(n=s)}return n}function Xr(i,e){let n;if(e===void 0)for(let r of i)r!=null&&(n>r||n===void 0&&r>=r)&&(n=r);else{let r=-1;for(let s of i)(s=e(s,++r,i))!=null&&(n>s||n===void 0&&s>=s)&&(n=s)}return n}function Su(i,e){let n=0;if(e===void 0)for(let r of i)(r=+r)&&(n+=r);else{let r=-1;for(let s of i)(s=+e(s,++r,i))&&(n+=s)}return n}function Ud(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function BC(i){if(Array.isArray(i))return i}function zC(i){if(Array.isArray(i))return Ud(i)}function pv(i,e,n){if(typeof i=="function"?i===e:i.has(e))return arguments.length<3?e:n;throw new TypeError("Private element is not present on this object")}function VC(i,e){if(e.has(i))throw new TypeError("Cannot initialize the same private elements twice on an object")}function GC(i,e){if(!(i instanceof e))throw new TypeError("Cannot call a class as a function")}function tn(i,e){return i.get(pv(i,e))}function Js(i,e,n){VC(i,e),e.set(i,n)}function wu(i,e,n){return i.set(pv(i,e),n),n}function HC(i,e){for(var n=0;n<e.length;n++){var r=e[n];r.enumerable=r.enumerable||!1,r.configurable=!0,"value"in r&&(r.writable=!0),Object.defineProperty(i,JC(r.key),r)}}function WC(i,e,n){return e&&HC(i.prototype,e),Object.defineProperty(i,"prototype",{writable:!1}),i}function XC(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function qC(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function jC(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function YC(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function $C(i,e){return BC(i)||qC(i,e)||mv(i,e)||jC()}function ZC(i){return zC(i)||XC(i)||mv(i)||YC()}function KC(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return String(i)}function JC(i){var e=KC(i,"string");return typeof e=="symbol"?e:e+""}function mv(i,e){if(i){if(typeof i=="string")return Ud(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Ud(i,e):void 0}}var Qs=new WeakMap,_a=new WeakMap,eo=new WeakMap,Dd=new WeakMap,Od=new WeakMap,Nd=new WeakMap,gv=(function(){function i(){GC(this,i),Js(this,Qs,new Map),Js(this,_a,new Map),Js(this,eo,function(e){return e}),Js(this,Dd,function(){return{}}),Js(this,Od,function(){}),Js(this,Nd,function(){})}return WC(i,[{key:"getObj",value:function(n){return tn(Qs,this).get(tn(eo,this).call(this,n))}},{key:"getData",value:function(n){return tn(_a,this).get(n)}},{key:"entries",value:function(){return ZC(tn(_a,this).entries()).map(function(n){var r=$C(n,2),s=r[0],o=r[1];return[o,s]})}},{key:"id",value:function(n){return wu(eo,this,we(n)),this}},{key:"onCreateObj",value:function(n){return wu(Dd,this,n),this}},{key:"onUpdateObj",value:function(n){return wu(Od,this,n),this}},{key:"onRemoveObj",value:function(n){return wu(Nd,this,n),this}},{key:"digest",value:function(n){var r=this;n.filter(function(o){return!tn(Qs,r).has(tn(eo,r).call(r,o))}).forEach(function(o){var a=tn(Dd,r).call(r,o);tn(Qs,r).set(tn(eo,r).call(r,o),a),tn(_a,r).set(a,o)});var s=new Map(n.map(function(o){return[tn(eo,r).call(r,o),o]}));return tn(Qs,this).forEach(function(o,a){s.has(a)?tn(Od,r).call(r,o,s.get(a)):(tn(Nd,r).call(r,o,a),tn(Qs,r).delete(a),tn(_a,r).delete(o))}),this}},{key:"clear",value:function(){return this.digest([]),this}}])})();function _v(i,e){switch(arguments.length){case 0:break;case 1:this.range(i);break;default:this.range(e).domain(i);break}return this}var Fd=Symbol("implicit");function qr(){var i=new Ks,e=[],n=[],r=Fd;function s(o){let a=i.get(o);if(a===void 0){if(r!==Fd)return r;i.set(o,a=e.push(o)-1)}return n[a%n.length]}return s.domain=function(o){if(!arguments.length)return e.slice();e=[],i=new Ks;for(let a of o)i.has(a)||i.set(a,e.push(a)-1);return s},s.range=function(o){return arguments.length?(n=Array.from(o),s):n.slice()},s.unknown=function(o){return arguments.length?(r=o,s):r},s.copy=function(){return qr(e,n).unknown(r)},_v.apply(s,arguments),s}function Mu(i,e,n){i.prototype=e.prototype=n,n.constructor=i}function kd(i,e){var n=Object.create(i.prototype);for(var r in e)n[r]=e[r];return n}function xa(){}var va=.7,Tu=1/va,to="\\s*([+-]?\\d+)\\s*",ya="\\s*([+-]?(?:\\d*\\.)?\\d+(?:[eE][+-]?\\d+)?)\\s*",_i="\\s*([+-]?(?:\\d*\\.)?\\d+(?:[eE][+-]?\\d+)?)%\\s*",QC=/^#([0-9a-f]{3,8})$/,eR=new RegExp(`^rgb\\(${to},${to},${to}\\)$`),tR=new RegExp(`^rgb\\(${_i},${_i},${_i}\\)$`),nR=new RegExp(`^rgba\\(${to},${to},${to},${ya}\\)$`),iR=new RegExp(`^rgba\\(${_i},${_i},${_i},${ya}\\)$`),rR=new RegExp(`^hsl\\(${ya},${_i},${_i}\\)$`),sR=new RegExp(`^hsla\\(${ya},${_i},${_i},${ya}\\)$`),vv={aliceblue:15792383,antiquewhite:16444375,aqua:65535,aquamarine:8388564,azure:15794175,beige:16119260,bisque:16770244,black:0,blanchedalmond:16772045,blue:255,blueviolet:9055202,brown:10824234,burlywood:14596231,cadetblue:6266528,chartreuse:8388352,chocolate:13789470,coral:16744272,cornflowerblue:6591981,cornsilk:16775388,crimson:14423100,cyan:65535,darkblue:139,darkcyan:35723,darkgoldenrod:12092939,darkgray:11119017,darkgreen:25600,darkgrey:11119017,darkkhaki:12433259,darkmagenta:9109643,darkolivegreen:5597999,darkorange:16747520,darkorchid:10040012,darkred:9109504,darksalmon:15308410,darkseagreen:9419919,darkslateblue:4734347,darkslategray:3100495,darkslategrey:3100495,darkturquoise:52945,darkviolet:9699539,deeppink:16716947,deepskyblue:49151,dimgray:6908265,dimgrey:6908265,dodgerblue:2003199,firebrick:11674146,floralwhite:16775920,forestgreen:2263842,fuchsia:16711935,gainsboro:14474460,ghostwhite:16316671,gold:16766720,goldenrod:14329120,gray:8421504,green:32768,greenyellow:11403055,grey:8421504,honeydew:15794160,hotpink:16738740,indianred:13458524,indigo:4915330,ivory:16777200,khaki:15787660,lavender:15132410,lavenderblush:16773365,lawngreen:8190976,lemonchiffon:16775885,lightblue:11393254,lightcoral:15761536,lightcyan:14745599,lightgoldenrodyellow:16448210,lightgray:13882323,lightgreen:9498256,lightgrey:13882323,lightpink:16758465,lightsalmon:16752762,lightseagreen:2142890,lightskyblue:8900346,lightslategray:7833753,lightslategrey:7833753,lightsteelblue:11584734,lightyellow:16777184,lime:65280,limegreen:3329330,linen:16445670,magenta:16711935,maroon:8388608,mediumaquamarine:6737322,mediumblue:205,mediumorchid:12211667,mediumpurple:9662683,mediumseagreen:3978097,mediumslateblue:8087790,mediumspringgreen:64154,mediumturquoise:4772300,mediumvioletred:13047173,midnightblue:1644912,mintcream:16121850,mistyrose:16770273,moccasin:16770229,navajowhite:16768685,navy:128,oldlace:16643558,olive:8421376,olivedrab:7048739,orange:16753920,orangered:16729344,orchid:14315734,palegoldenrod:15657130,palegreen:10025880,paleturquoise:11529966,palevioletred:14381203,papayawhip:16773077,peachpuff:16767673,peru:13468991,pink:16761035,plum:14524637,powderblue:11591910,purple:8388736,rebeccapurple:6697881,red:16711680,rosybrown:12357519,royalblue:4286945,saddlebrown:9127187,salmon:16416882,sandybrown:16032864,seagreen:3050327,seashell:16774638,sienna:10506797,silver:12632256,skyblue:8900331,slateblue:6970061,slategray:7372944,slategrey:7372944,snow:16775930,springgreen:65407,steelblue:4620980,tan:13808780,teal:32896,thistle:14204888,tomato:16737095,turquoise:4251856,violet:15631086,wheat:16113331,white:16777215,whitesmoke:16119285,yellow:16776960,yellowgreen:10145074};Mu(xa,hr,{copy(i){return Object.assign(new this.constructor,this,i)},displayable(){return this.rgb().displayable()},hex:yv,formatHex:yv,formatHex8:oR,formatHsl:aR,formatRgb:xv,toString:xv});function yv(){return this.rgb().formatHex()}function oR(){return this.rgb().formatHex8()}function aR(){return Av(this).formatHsl()}function xv(){return this.rgb().formatRgb()}function hr(i){var e,n;return i=(i+"").trim().toLowerCase(),(e=QC.exec(i))?(n=e[1].length,e=parseInt(e[1],16),n===6?bv(e):n===3?new _n(e>>8&15|e>>4&240,e>>4&15|e&240,(e&15)<<4|e&15,1):n===8?Eu(e>>24&255,e>>16&255,e>>8&255,(e&255)/255):n===4?Eu(e>>12&15|e>>8&240,e>>8&15|e>>4&240,e>>4&15|e&240,((e&15)<<4|e&15)/255):null):(e=eR.exec(i))?new _n(e[1],e[2],e[3],1):(e=tR.exec(i))?new _n(e[1]*255/100,e[2]*255/100,e[3]*255/100,1):(e=nR.exec(i))?Eu(e[1],e[2],e[3],e[4]):(e=iR.exec(i))?Eu(e[1]*255/100,e[2]*255/100,e[3]*255/100,e[4]):(e=rR.exec(i))?Mv(e[1],e[2]/100,e[3]/100,1):(e=sR.exec(i))?Mv(e[1],e[2]/100,e[3]/100,e[4]):vv.hasOwnProperty(i)?bv(vv[i]):i==="transparent"?new _n(NaN,NaN,NaN,0):null}function bv(i){return new _n(i>>16&255,i>>8&255,i&255,1)}function Eu(i,e,n,r){return r<=0&&(i=e=n=NaN),new _n(i,e,n,r)}function lR(i){return i instanceof xa||(i=hr(i)),i?(i=i.rgb(),new _n(i.r,i.g,i.b,i.opacity)):new _n}function no(i,e,n,r){return arguments.length===1?lR(i):new _n(i,e,n,r??1)}function _n(i,e,n,r){this.r=+i,this.g=+e,this.b=+n,this.opacity=+r}Mu(_n,no,kd(xa,{brighter(i){return i=i==null?Tu:Math.pow(Tu,i),new _n(this.r*i,this.g*i,this.b*i,this.opacity)},darker(i){return i=i==null?va:Math.pow(va,i),new _n(this.r*i,this.g*i,this.b*i,this.opacity)},rgb(){return this},clamp(){return new _n(Yr(this.r),Yr(this.g),Yr(this.b),Cu(this.opacity))},displayable(){return-.5<=this.r&&this.r<255.5&&-.5<=this.g&&this.g<255.5&&-.5<=this.b&&this.b<255.5&&0<=this.opacity&&this.opacity<=1},hex:Sv,formatHex:Sv,formatHex8:cR,formatRgb:wv,toString:wv}));function Sv(){return`#${jr(this.r)}${jr(this.g)}${jr(this.b)}`}function cR(){return`#${jr(this.r)}${jr(this.g)}${jr(this.b)}${jr((isNaN(this.opacity)?1:this.opacity)*255)}`}function wv(){let i=Cu(this.opacity);return`${i===1?"rgb(":"rgba("}${Yr(this.r)}, ${Yr(this.g)}, ${Yr(this.b)}${i===1?")":`, ${i})`}`}function Cu(i){return isNaN(i)?1:Math.max(0,Math.min(1,i))}function Yr(i){return Math.max(0,Math.min(255,Math.round(i)||0))}function jr(i){return i=Yr(i),(i<16?"0":"")+i.toString(16)}function Mv(i,e,n,r){return r<=0?i=e=n=NaN:n<=0||n>=1?i=e=NaN:e<=0&&(i=NaN),new ti(i,e,n,r)}function Av(i){if(i instanceof ti)return new ti(i.h,i.s,i.l,i.opacity);if(i instanceof xa||(i=hr(i)),!i)return new ti;if(i instanceof ti)return i;i=i.rgb();var e=i.r/255,n=i.g/255,r=i.b/255,s=Math.min(e,n,r),o=Math.max(e,n,r),a=NaN,l=o-s,c=(o+s)/2;return l?(e===o?a=(n-r)/l+(n<r)*6:n===o?a=(r-e)/l+2:a=(e-n)/l+4,l/=c<.5?o+s:2-o-s,a*=60):l=c>0&&c<1?0:a,new ti(a,l,c,i.opacity)}function Tv(i,e,n,r){return arguments.length===1?Av(i):new ti(i,e,n,r??1)}function ti(i,e,n,r){this.h=+i,this.s=+e,this.l=+n,this.opacity=+r}Mu(ti,Tv,kd(xa,{brighter(i){return i=i==null?Tu:Math.pow(Tu,i),new ti(this.h,this.s,this.l*i,this.opacity)},darker(i){return i=i==null?va:Math.pow(va,i),new ti(this.h,this.s,this.l*i,this.opacity)},rgb(){var i=this.h%360+(this.h<0)*360,e=isNaN(i)||isNaN(this.s)?0:this.s,n=this.l,r=n+(n<.5?n:1-n)*e,s=2*n-r;return new _n(Bd(i>=240?i-240:i+120,s,r),Bd(i,s,r),Bd(i<120?i+240:i-120,s,r),this.opacity)},clamp(){return new ti(Ev(this.h),Au(this.s),Au(this.l),Cu(this.opacity))},displayable(){return(0<=this.s&&this.s<=1||isNaN(this.s))&&0<=this.l&&this.l<=1&&0<=this.opacity&&this.opacity<=1},formatHsl(){let i=Cu(this.opacity);return`${i===1?"hsl(":"hsla("}${Ev(this.h)}, ${Au(this.s)*100}%, ${Au(this.l)*100}%${i===1?")":`, ${i})`}`}}));function Ev(i){return i=(i||0)%360,i<0?i+360:i}function Au(i){return Math.max(0,Math.min(1,i||0))}function Bd(i,e,n){return(i<60?e+(n-e)*i/60:i<180?n:i<240?e+(n-e)*(240-i)/60:e)*255}function zd(i,e,n,r,s){var o=i*i,a=o*i;return((1-3*i+3*o-a)*e+(4-6*o+3*a)*n+(1+3*i+3*o-3*a)*r+a*s)/6}function Cv(i){var e=i.length-1;return function(n){var r=n<=0?n=0:n>=1?(n=1,e-1):Math.floor(n*e),s=i[r],o=i[r+1],a=r>0?i[r-1]:2*s-o,l=r<e-1?i[r+2]:2*o-s;return zd((n-r/e)*e,a,s,o,l)}}function Rv(i){var e=i.length;return function(n){var r=Math.floor(((n%=1)<0?++n:n)*e),s=i[(r+e-1)%e],o=i[r%e],a=i[(r+1)%e],l=i[(r+2)%e];return zd((n-r/e)*e,s,o,a,l)}}var Vd=i=>()=>i;function uR(i,e){return function(n){return i+n*e}}function hR(i,e,n){return i=Math.pow(i,n),e=Math.pow(e,n)-i,n=1/n,function(r){return Math.pow(i+r*e,n)}}function Pv(i){return(i=+i)==1?Ru:function(e,n){return n-e?hR(e,n,i):Vd(isNaN(e)?n:e)}}function Ru(i,e){var n=e-i;return n?uR(i,n):Vd(isNaN(i)?e:i)}var Pu=(function i(e){var n=Pv(e);function r(s,o){var a=n((s=no(s)).r,(o=no(o)).r),l=n(s.g,o.g),c=n(s.b,o.b),u=Ru(s.opacity,o.opacity);return function(h){return s.r=a(h),s.g=l(h),s.b=c(h),s.opacity=u(h),s+""}}return r.gamma=i,r})(1);function Iv(i){return function(e){var n=e.length,r=new Array(n),s=new Array(n),o=new Array(n),a,l;for(a=0;a<n;++a)l=no(e[a]),r[a]=l.r||0,s[a]=l.g||0,o[a]=l.b||0;return r=i(r),s=i(s),o=i(o),l.opacity=1,function(c){return l.r=r(c),l.g=s(c),l.b=o(c),l+""}}}var fR=Iv(Cv),dR=Iv(Rv);function kn(i,e){return i=+i,e=+e,function(n){return i*(1-n)+e*n}}var Hd=/[-+]?(?:\d+\.?\d*|\.?\d+)(?:[eE][-+]?\d+)?/g,Gd=new RegExp(Hd.source,"g");function pR(i){return function(){return i}}function mR(i){return function(e){return i(e)+""}}function Wd(i,e){var n=Hd.lastIndex=Gd.lastIndex=0,r,s,o,a=-1,l=[],c=[];for(i=i+"",e=e+"";(r=Hd.exec(i))&&(s=Gd.exec(e));)(o=s.index)>n&&(o=e.slice(n,o),l[a]?l[a]+=o:l[++a]=o),(r=r[0])===(s=s[0])?l[a]?l[a]+=s:l[++a]=s:(l[++a]=null,c.push({i:a,x:kn(r,s)})),n=Gd.lastIndex;return n<e.length&&(o=e.slice(n),l[a]?l[a]+=o:l[++a]=o),l.length<2?c[0]?mR(c[0].x):pR(e):(e=c.length,function(u){for(var h=0,d;h<e;++h)l[(d=c[h]).i]=d.x(u);return l.join("")})}var Lv=180/Math.PI,Iu={translateX:0,translateY:0,rotate:0,skewX:0,scaleX:1,scaleY:1};function Xd(i,e,n,r,s,o){var a,l,c;return(a=Math.sqrt(i*i+e*e))&&(i/=a,e/=a),(c=i*n+e*r)&&(n-=i*c,r-=e*c),(l=Math.sqrt(n*n+r*r))&&(n/=l,r/=l,c/=l),i*r<e*n&&(i=-i,e=-e,c=-c,a=-a),{translateX:s,translateY:o,rotate:Math.atan2(e,i)*Lv,skewX:Math.atan(c)*Lv,scaleX:a,scaleY:l}}var Lu;function Dv(i){let e=new(typeof DOMMatrix=="function"?DOMMatrix:WebKitCSSMatrix)(i+"");return e.isIdentity?Iu:Xd(e.a,e.b,e.c,e.d,e.e,e.f)}function Ov(i){return i==null?Iu:(Lu||(Lu=document.createElementNS("http://www.w3.org/2000/svg","g")),Lu.setAttribute("transform",i),(i=Lu.transform.baseVal.consolidate())?(i=i.matrix,Xd(i.a,i.b,i.c,i.d,i.e,i.f)):Iu)}function Nv(i,e,n,r){function s(u){return u.length?u.pop()+" ":""}function o(u,h,d,f,p,g){if(u!==d||h!==f){var v=p.push("translate(",null,e,null,n);g.push({i:v-4,x:kn(u,d)},{i:v-2,x:kn(h,f)})}else(d||f)&&p.push("translate("+d+e+f+n)}function a(u,h,d,f){u!==h?(u-h>180?h+=360:h-u>180&&(u+=360),f.push({i:d.push(s(d)+"rotate(",null,r)-2,x:kn(u,h)})):h&&d.push(s(d)+"rotate("+h+r)}function l(u,h,d,f){u!==h?f.push({i:d.push(s(d)+"skewX(",null,r)-2,x:kn(u,h)}):h&&d.push(s(d)+"skewX("+h+r)}function c(u,h,d,f,p,g){if(u!==d||h!==f){var v=p.push(s(p)+"scale(",null,",",null,")");g.push({i:v-4,x:kn(u,d)},{i:v-2,x:kn(h,f)})}else(d!==1||f!==1)&&p.push(s(p)+"scale("+d+","+f+")")}return function(u,h){var d=[],f=[];return u=i(u),h=i(h),o(u.translateX,u.translateY,h.translateX,h.translateY,d,f),a(u.rotate,h.rotate,d,f),l(u.skewX,h.skewX,d,f),c(u.scaleX,u.scaleY,h.scaleX,h.scaleY,d,f),u=h=null,function(p){for(var g=-1,v=f.length,_;++g<v;)d[(_=f[g]).i]=_.x(p);return d.join("")}}}var qd=Nv(Dv,"px, ","px)","deg)"),jd=Nv(Ov,", ",")",")");var gR=1e-12;function Uv(i){return((i=Math.exp(i))+1/i)/2}function _R(i){return((i=Math.exp(i))-1/i)/2}function vR(i){return((i=Math.exp(2*i))-1)/(i+1)}var Yd=(function i(e,n,r){function s(o,a){var l=o[0],c=o[1],u=o[2],h=a[0],d=a[1],f=a[2],p=h-l,g=d-c,v=p*p+g*g,_,m;if(v<gR)m=Math.log(f/u)/e,_=function(A){return[l+A*p,c+A*g,u*Math.exp(e*A*m)]};else{var w=Math.sqrt(v),T=(f*f-u*u+r*v)/(2*u*n*w),y=(f*f-u*u-r*v)/(2*f*n*w),b=Math.log(Math.sqrt(T*T+1)-T),S=Math.log(Math.sqrt(y*y+1)-y);m=(S-b)/e,_=function(A){var x=A*m,C=Uv(b),L=u/(n*w)*(C*vR(e*x+b)-_R(b));return[l+L*p,c+L*g,u*C/Uv(e*x+b)]}}return _.duration=m*1e3*e/Math.SQRT2,_}return s.rho=function(o){var a=Math.max(.001,+o),l=a*a,c=l*l;return i(a,l,c)},s})(Math.SQRT2,2,4);function Fv(i){for(var e=i.length/6|0,n=new Array(e),r=0;r<e;)n[r]="#"+i.slice(r*6,++r*6);return n}var ba=Fv("a6cee31f78b4b2df8a33a02cfb9a99e31a1cfdbf6fff7f00cab2d66a3d9affff99b15928");function Du(i){"@babel/helpers - typeof";return Du=typeof Symbol=="function"&&typeof Symbol.iterator=="symbol"?function(e){return typeof e}:function(e){return e&&typeof Symbol=="function"&&e.constructor===Symbol&&e!==Symbol.prototype?"symbol":typeof e},Du(i)}var yR=/^\s+/,xR=/\s+$/;function Pe(i,e){if(i=i||"",e=e||{},i instanceof Pe)return i;if(!(this instanceof Pe))return new Pe(i,e);var n=bR(i);this._originalInput=i,this._r=n.r,this._g=n.g,this._b=n.b,this._a=n.a,this._roundA=Math.round(100*this._a)/100,this._format=e.format||n.format,this._gradientType=e.gradientType,this._r<1&&(this._r=Math.round(this._r)),this._g<1&&(this._g=Math.round(this._g)),this._b<1&&(this._b=Math.round(this._b)),this._ok=n.ok}Pe.prototype={isDark:function(){return this.getBrightness()<128},isLight:function(){return!this.isDark()},isValid:function(){return this._ok},getOriginalInput:function(){return this._originalInput},getFormat:function(){return this._format},getAlpha:function(){return this._a},getBrightness:function(){var e=this.toRgb();return(e.r*299+e.g*587+e.b*114)/1e3},getLuminance:function(){var e=this.toRgb(),n,r,s,o,a,l;return n=e.r/255,r=e.g/255,s=e.b/255,n<=.03928?o=n/12.92:o=Math.pow((n+.055)/1.055,2.4),r<=.03928?a=r/12.92:a=Math.pow((r+.055)/1.055,2.4),s<=.03928?l=s/12.92:l=Math.pow((s+.055)/1.055,2.4),.2126*o+.7152*a+.0722*l},setAlpha:function(e){return this._a=Wv(e),this._roundA=Math.round(100*this._a)/100,this},toHsv:function(){var e=Bv(this._r,this._g,this._b);return{h:e.h*360,s:e.s,v:e.v,a:this._a}},toHsvString:function(){var e=Bv(this._r,this._g,this._b),n=Math.round(e.h*360),r=Math.round(e.s*100),s=Math.round(e.v*100);return this._a==1?"hsv("+n+", "+r+"%, "+s+"%)":"hsva("+n+", "+r+"%, "+s+"%, "+this._roundA+")"},toHsl:function(){var e=kv(this._r,this._g,this._b);return{h:e.h*360,s:e.s,l:e.l,a:this._a}},toHslString:function(){var e=kv(this._r,this._g,this._b),n=Math.round(e.h*360),r=Math.round(e.s*100),s=Math.round(e.l*100);return this._a==1?"hsl("+n+", "+r+"%, "+s+"%)":"hsla("+n+", "+r+"%, "+s+"%, "+this._roundA+")"},toHex:function(e){return zv(this._r,this._g,this._b,e)},toHexString:function(e){return"#"+this.toHex(e)},toHex8:function(e){return ER(this._r,this._g,this._b,this._a,e)},toHex8String:function(e){return"#"+this.toHex8(e)},toRgb:function(){return{r:Math.round(this._r),g:Math.round(this._g),b:Math.round(this._b),a:this._a}},toRgbString:function(){return this._a==1?"rgb("+Math.round(this._r)+", "+Math.round(this._g)+", "+Math.round(this._b)+")":"rgba("+Math.round(this._r)+", "+Math.round(this._g)+", "+Math.round(this._b)+", "+this._roundA+")"},toPercentageRgb:function(){return{r:Math.round(yt(this._r,255)*100)+"%",g:Math.round(yt(this._g,255)*100)+"%",b:Math.round(yt(this._b,255)*100)+"%",a:this._a}},toPercentageRgbString:function(){return this._a==1?"rgb("+Math.round(yt(this._r,255)*100)+"%, "+Math.round(yt(this._g,255)*100)+"%, "+Math.round(yt(this._b,255)*100)+"%)":"rgba("+Math.round(yt(this._r,255)*100)+"%, "+Math.round(yt(this._g,255)*100)+"%, "+Math.round(yt(this._b,255)*100)+"%, "+this._roundA+")"},toName:function(){return this._a===0?"transparent":this._a<1?!1:FR[zv(this._r,this._g,this._b,!0)]||!1},toFilter:function(e){var n="#"+Vv(this._r,this._g,this._b,this._a),r=n,s=this._gradientType?"GradientType = 1, ":"";if(e){var o=Pe(e);r="#"+Vv(o._r,o._g,o._b,o._a)}return"progid:DXImageTransform.Microsoft.gradient("+s+"startColorstr="+n+",endColorstr="+r+")"},toString:function(e){var n=!!e;e=e||this._format;var r=!1,s=this._a<1&&this._a>=0,o=!n&&s&&(e==="hex"||e==="hex6"||e==="hex3"||e==="hex4"||e==="hex8"||e==="name");return o?e==="name"&&this._a===0?this.toName():this.toRgbString():(e==="rgb"&&(r=this.toRgbString()),e==="prgb"&&(r=this.toPercentageRgbString()),(e==="hex"||e==="hex6")&&(r=this.toHexString()),e==="hex3"&&(r=this.toHexString(!0)),e==="hex4"&&(r=this.toHex8String(!0)),e==="hex8"&&(r=this.toHex8String()),e==="name"&&(r=this.toName()),e==="hsl"&&(r=this.toHslString()),e==="hsv"&&(r=this.toHsvString()),r||this.toHexString())},clone:function(){return Pe(this.toString())},_applyModification:function(e,n){var r=e.apply(null,[this].concat([].slice.call(n)));return this._r=r._r,this._g=r._g,this._b=r._b,this.setAlpha(r._a),this},lighten:function(){return this._applyModification(RR,arguments)},brighten:function(){return this._applyModification(PR,arguments)},darken:function(){return this._applyModification(IR,arguments)},desaturate:function(){return this._applyModification(AR,arguments)},saturate:function(){return this._applyModification(TR,arguments)},greyscale:function(){return this._applyModification(CR,arguments)},spin:function(){return this._applyModification(LR,arguments)},_applyCombination:function(e,n){return e.apply(null,[this].concat([].slice.call(n)))},analogous:function(){return this._applyCombination(NR,arguments)},complement:function(){return this._applyCombination(DR,arguments)},monochromatic:function(){return this._applyCombination(UR,arguments)},splitcomplement:function(){return this._applyCombination(OR,arguments)},triad:function(){return this._applyCombination(Gv,[3])},tetrad:function(){return this._applyCombination(Gv,[4])}};Pe.fromRatio=function(i,e){if(Du(i)=="object"){var n={};for(var r in i)i.hasOwnProperty(r)&&(r==="a"?n[r]=i[r]:n[r]=Sa(i[r]));i=n}return Pe(i,e)};function bR(i){var e={r:0,g:0,b:0},n=1,r=null,s=null,o=null,a=!1,l=!1;return typeof i=="string"&&(i=VR(i)),Du(i)=="object"&&(Oi(i.r)&&Oi(i.g)&&Oi(i.b)?(e=SR(i.r,i.g,i.b),a=!0,l=String(i.r).substr(-1)==="%"?"prgb":"rgb"):Oi(i.h)&&Oi(i.s)&&Oi(i.v)?(r=Sa(i.s),s=Sa(i.v),e=MR(i.h,r,s),a=!0,l="hsv"):Oi(i.h)&&Oi(i.s)&&Oi(i.l)&&(r=Sa(i.s),o=Sa(i.l),e=wR(i.h,r,o),a=!0,l="hsl"),i.hasOwnProperty("a")&&(n=i.a)),n=Wv(n),{ok:a,format:i.format||l,r:Math.min(255,Math.max(e.r,0)),g:Math.min(255,Math.max(e.g,0)),b:Math.min(255,Math.max(e.b,0)),a:n}}function SR(i,e,n){return{r:yt(i,255)*255,g:yt(e,255)*255,b:yt(n,255)*255}}function kv(i,e,n){i=yt(i,255),e=yt(e,255),n=yt(n,255);var r=Math.max(i,e,n),s=Math.min(i,e,n),o,a,l=(r+s)/2;if(r==s)o=a=0;else{var c=r-s;switch(a=l>.5?c/(2-r-s):c/(r+s),r){case i:o=(e-n)/c+(e<n?6:0);break;case e:o=(n-i)/c+2;break;case n:o=(i-e)/c+4;break}o/=6}return{h:o,s:a,l}}function wR(i,e,n){var r,s,o;i=yt(i,360),e=yt(e,100),n=yt(n,100);function a(u,h,d){return d<0&&(d+=1),d>1&&(d-=1),d<1/6?u+(h-u)*6*d:d<1/2?h:d<2/3?u+(h-u)*(2/3-d)*6:u}if(e===0)r=s=o=n;else{var l=n<.5?n*(1+e):n+e-n*e,c=2*n-l;r=a(c,l,i+1/3),s=a(c,l,i),o=a(c,l,i-1/3)}return{r:r*255,g:s*255,b:o*255}}function Bv(i,e,n){i=yt(i,255),e=yt(e,255),n=yt(n,255);var r=Math.max(i,e,n),s=Math.min(i,e,n),o,a,l=r,c=r-s;if(a=r===0?0:c/r,r==s)o=0;else{switch(r){case i:o=(e-n)/c+(e<n?6:0);break;case e:o=(n-i)/c+2;break;case n:o=(i-e)/c+4;break}o/=6}return{h:o,s:a,v:l}}function MR(i,e,n){i=yt(i,360)*6,e=yt(e,100),n=yt(n,100);var r=Math.floor(i),s=i-r,o=n*(1-e),a=n*(1-s*e),l=n*(1-(1-s)*e),c=r%6,u=[n,a,o,o,l,n][c],h=[l,n,n,a,o,o][c],d=[o,o,l,n,n,a][c];return{r:u*255,g:h*255,b:d*255}}function zv(i,e,n,r){var s=[ii(Math.round(i).toString(16)),ii(Math.round(e).toString(16)),ii(Math.round(n).toString(16))];return r&&s[0].charAt(0)==s[0].charAt(1)&&s[1].charAt(0)==s[1].charAt(1)&&s[2].charAt(0)==s[2].charAt(1)?s[0].charAt(0)+s[1].charAt(0)+s[2].charAt(0):s.join("")}function ER(i,e,n,r,s){var o=[ii(Math.round(i).toString(16)),ii(Math.round(e).toString(16)),ii(Math.round(n).toString(16)),ii(Xv(r))];return s&&o[0].charAt(0)==o[0].charAt(1)&&o[1].charAt(0)==o[1].charAt(1)&&o[2].charAt(0)==o[2].charAt(1)&&o[3].charAt(0)==o[3].charAt(1)?o[0].charAt(0)+o[1].charAt(0)+o[2].charAt(0)+o[3].charAt(0):o.join("")}function Vv(i,e,n,r){var s=[ii(Xv(r)),ii(Math.round(i).toString(16)),ii(Math.round(e).toString(16)),ii(Math.round(n).toString(16))];return s.join("")}Pe.equals=function(i,e){return!i||!e?!1:Pe(i).toRgbString()==Pe(e).toRgbString()};Pe.random=function(){return Pe.fromRatio({r:Math.random(),g:Math.random(),b:Math.random()})};function AR(i,e){e=e===0?0:e||10;var n=Pe(i).toHsl();return n.s-=e/100,n.s=Ou(n.s),Pe(n)}function TR(i,e){e=e===0?0:e||10;var n=Pe(i).toHsl();return n.s+=e/100,n.s=Ou(n.s),Pe(n)}function CR(i){return Pe(i).desaturate(100)}function RR(i,e){e=e===0?0:e||10;var n=Pe(i).toHsl();return n.l+=e/100,n.l=Ou(n.l),Pe(n)}function PR(i,e){e=e===0?0:e||10;var n=Pe(i).toRgb();return n.r=Math.max(0,Math.min(255,n.r-Math.round(255*-(e/100)))),n.g=Math.max(0,Math.min(255,n.g-Math.round(255*-(e/100)))),n.b=Math.max(0,Math.min(255,n.b-Math.round(255*-(e/100)))),Pe(n)}function IR(i,e){e=e===0?0:e||10;var n=Pe(i).toHsl();return n.l-=e/100,n.l=Ou(n.l),Pe(n)}function LR(i,e){var n=Pe(i).toHsl(),r=(n.h+e)%360;return n.h=r<0?360+r:r,Pe(n)}function DR(i){var e=Pe(i).toHsl();return e.h=(e.h+180)%360,Pe(e)}function Gv(i,e){if(isNaN(e)||e<=0)throw new Error("Argument to polyad must be a positive number");for(var n=Pe(i).toHsl(),r=[Pe(i)],s=360/e,o=1;o<e;o++)r.push(Pe({h:(n.h+o*s)%360,s:n.s,l:n.l}));return r}function OR(i){var e=Pe(i).toHsl(),n=e.h;return[Pe(i),Pe({h:(n+72)%360,s:e.s,l:e.l}),Pe({h:(n+216)%360,s:e.s,l:e.l})]}function NR(i,e,n){e=e||6,n=n||30;var r=Pe(i).toHsl(),s=360/n,o=[Pe(i)];for(r.h=(r.h-(s*e>>1)+720)%360;--e;)r.h=(r.h+s)%360,o.push(Pe(r));return o}function UR(i,e){e=e||6;for(var n=Pe(i).toHsv(),r=n.h,s=n.s,o=n.v,a=[],l=1/e;e--;)a.push(Pe({h:r,s,v:o})),o=(o+l)%1;return a}Pe.mix=function(i,e,n){n=n===0?0:n||50;var r=Pe(i).toRgb(),s=Pe(e).toRgb(),o=n/100,a={r:(s.r-r.r)*o+r.r,g:(s.g-r.g)*o+r.g,b:(s.b-r.b)*o+r.b,a:(s.a-r.a)*o+r.a};return Pe(a)};Pe.readability=function(i,e){var n=Pe(i),r=Pe(e);return(Math.max(n.getLuminance(),r.getLuminance())+.05)/(Math.min(n.getLuminance(),r.getLuminance())+.05)};Pe.isReadable=function(i,e,n){var r=Pe.readability(i,e),s,o;switch(o=!1,s=GR(n),s.level+s.size){case"AAsmall":case"AAAlarge":o=r>=4.5;break;case"AAlarge":o=r>=3;break;case"AAAsmall":o=r>=7;break}return o};Pe.mostReadable=function(i,e,n){var r=null,s=0,o,a,l,c;n=n||{},a=n.includeFallbackColors,l=n.level,c=n.size;for(var u=0;u<e.length;u++)o=Pe.readability(i,e[u]),o>s&&(s=o,r=Pe(e[u]));return Pe.isReadable(i,r,{level:l,size:c})||!a?r:(n.includeFallbackColors=!1,Pe.mostReadable(i,["#fff","#000"],n))};var $d=Pe.names={aliceblue:"f0f8ff",antiquewhite:"faebd7",aqua:"0ff",aquamarine:"7fffd4",azure:"f0ffff",beige:"f5f5dc",bisque:"ffe4c4",black:"000",blanchedalmond:"ffebcd",blue:"00f",blueviolet:"8a2be2",brown:"a52a2a",burlywood:"deb887",burntsienna:"ea7e5d",cadetblue:"5f9ea0",chartreuse:"7fff00",chocolate:"d2691e",coral:"ff7f50",cornflowerblue:"6495ed",cornsilk:"fff8dc",crimson:"dc143c",cyan:"0ff",darkblue:"00008b",darkcyan:"008b8b",darkgoldenrod:"b8860b",darkgray:"a9a9a9",darkgreen:"006400",darkgrey:"a9a9a9",darkkhaki:"bdb76b",darkmagenta:"8b008b",darkolivegreen:"556b2f",darkorange:"ff8c00",darkorchid:"9932cc",darkred:"8b0000",darksalmon:"e9967a",darkseagreen:"8fbc8f",darkslateblue:"483d8b",darkslategray:"2f4f4f",darkslategrey:"2f4f4f",darkturquoise:"00ced1",darkviolet:"9400d3",deeppink:"ff1493",deepskyblue:"00bfff",dimgray:"696969",dimgrey:"696969",dodgerblue:"1e90ff",firebrick:"b22222",floralwhite:"fffaf0",forestgreen:"228b22",fuchsia:"f0f",gainsboro:"dcdcdc",ghostwhite:"f8f8ff",gold:"ffd700",goldenrod:"daa520",gray:"808080",green:"008000",greenyellow:"adff2f",grey:"808080",honeydew:"f0fff0",hotpink:"ff69b4",indianred:"cd5c5c",indigo:"4b0082",ivory:"fffff0",khaki:"f0e68c",lavender:"e6e6fa",lavenderblush:"fff0f5",lawngreen:"7cfc00",lemonchiffon:"fffacd",lightblue:"add8e6",lightcoral:"f08080",lightcyan:"e0ffff",lightgoldenrodyellow:"fafad2",lightgray:"d3d3d3",lightgreen:"90ee90",lightgrey:"d3d3d3",lightpink:"ffb6c1",lightsalmon:"ffa07a",lightseagreen:"20b2aa",lightskyblue:"87cefa",lightslategray:"789",lightslategrey:"789",lightsteelblue:"b0c4de",lightyellow:"ffffe0",lime:"0f0",limegreen:"32cd32",linen:"faf0e6",magenta:"f0f",maroon:"800000",mediumaquamarine:"66cdaa",mediumblue:"0000cd",mediumorchid:"ba55d3",mediumpurple:"9370db",mediumseagreen:"3cb371",mediumslateblue:"7b68ee",mediumspringgreen:"00fa9a",mediumturquoise:"48d1cc",mediumvioletred:"c71585",midnightblue:"191970",mintcream:"f5fffa",mistyrose:"ffe4e1",moccasin:"ffe4b5",navajowhite:"ffdead",navy:"000080",oldlace:"fdf5e6",olive:"808000",olivedrab:"6b8e23",orange:"ffa500",orangered:"ff4500",orchid:"da70d6",palegoldenrod:"eee8aa",palegreen:"98fb98",paleturquoise:"afeeee",palevioletred:"db7093",papayawhip:"ffefd5",peachpuff:"ffdab9",peru:"cd853f",pink:"ffc0cb",plum:"dda0dd",powderblue:"b0e0e6",purple:"800080",rebeccapurple:"663399",red:"f00",rosybrown:"bc8f8f",royalblue:"4169e1",saddlebrown:"8b4513",salmon:"fa8072",sandybrown:"f4a460",seagreen:"2e8b57",seashell:"fff5ee",sienna:"a0522d",silver:"c0c0c0",skyblue:"87ceeb",slateblue:"6a5acd",slategray:"708090",slategrey:"708090",snow:"fffafa",springgreen:"00ff7f",steelblue:"4682b4",tan:"d2b48c",teal:"008080",thistle:"d8bfd8",tomato:"ff6347",turquoise:"40e0d0",violet:"ee82ee",wheat:"f5deb3",white:"fff",whitesmoke:"f5f5f5",yellow:"ff0",yellowgreen:"9acd32"},FR=Pe.hexNames=kR($d);function kR(i){var e={};for(var n in i)i.hasOwnProperty(n)&&(e[i[n]]=n);return e}function Wv(i){return i=parseFloat(i),(isNaN(i)||i<0||i>1)&&(i=1),i}function yt(i,e){BR(i)&&(i="100%");var n=zR(i);return i=Math.min(e,Math.max(0,parseFloat(i))),n&&(i=parseInt(i*e,10)/100),Math.abs(i-e)<1e-6?1:i%e/parseFloat(e)}function Ou(i){return Math.min(1,Math.max(0,i))}function An(i){return parseInt(i,16)}function BR(i){return typeof i=="string"&&i.indexOf(".")!=-1&&parseFloat(i)===1}function zR(i){return typeof i=="string"&&i.indexOf("%")!=-1}function ii(i){return i.length==1?"0"+i:""+i}function Sa(i){return i<=1&&(i=i*100+"%"),i}function Xv(i){return Math.round(parseFloat(i)*255).toString(16)}function Hv(i){return An(i)/255}var ni=(function(){var i="[-\\+]?\\d+%?",e="[-\\+]?\\d*\\.\\d+%?",n="(?:"+e+")|(?:"+i+")",r="[\\s|\\(]+("+n+")[,|\\s]+("+n+")[,|\\s]+("+n+")\\s*\\)?",s="[\\s|\\(]+("+n+")[,|\\s]+("+n+")[,|\\s]+("+n+")[,|\\s]+("+n+")\\s*\\)?";return{CSS_UNIT:new RegExp(n),rgb:new RegExp("rgb"+r),rgba:new RegExp("rgba"+s),hsl:new RegExp("hsl"+r),hsla:new RegExp("hsla"+s),hsv:new RegExp("hsv"+r),hsva:new RegExp("hsva"+s),hex3:/^#?([0-9a-fA-F]{1})([0-9a-fA-F]{1})([0-9a-fA-F]{1})$/,hex6:/^#?([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})$/,hex4:/^#?([0-9a-fA-F]{1})([0-9a-fA-F]{1})([0-9a-fA-F]{1})([0-9a-fA-F]{1})$/,hex8:/^#?([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})$/}})();function Oi(i){return!!ni.CSS_UNIT.exec(i)}function VR(i){i=i.replace(yR,"").replace(xR,"").toLowerCase();var e=!1;if($d[i])i=$d[i],e=!0;else if(i=="transparent")return{r:0,g:0,b:0,a:0,format:"name"};var n;return(n=ni.rgb.exec(i))?{r:n[1],g:n[2],b:n[3]}:(n=ni.rgba.exec(i))?{r:n[1],g:n[2],b:n[3],a:n[4]}:(n=ni.hsl.exec(i))?{h:n[1],s:n[2],l:n[3]}:(n=ni.hsla.exec(i))?{h:n[1],s:n[2],l:n[3],a:n[4]}:(n=ni.hsv.exec(i))?{h:n[1],s:n[2],v:n[3]}:(n=ni.hsva.exec(i))?{h:n[1],s:n[2],v:n[3],a:n[4]}:(n=ni.hex8.exec(i))?{r:An(n[1]),g:An(n[2]),b:An(n[3]),a:Hv(n[4]),format:e?"name":"hex8"}:(n=ni.hex6.exec(i))?{r:An(n[1]),g:An(n[2]),b:An(n[3]),format:e?"name":"hex"}:(n=ni.hex4.exec(i))?{r:An(n[1]+""+n[1]),g:An(n[2]+""+n[2]),b:An(n[3]+""+n[3]),a:Hv(n[4]+""+n[4]),format:e?"name":"hex8"}:(n=ni.hex3.exec(i))?{r:An(n[1]+""+n[1]),g:An(n[2]+""+n[2]),b:An(n[3]+""+n[3]),format:e?"name":"hex"}:!1}function GR(i){var e,n;return i=i||{level:"AA",size:"small"},e=(i.level||"AA").toUpperCase(),n=(i.size||"small").toLowerCase(),e!=="AA"&&e!=="AAA"&&(e="AA"),n!=="small"&&n!=="large"&&(n="small"),{level:e,size:n}}function tp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function HR(i){if(Array.isArray(i))return i}function WR(i){if(Array.isArray(i))return tp(i)}function Qv(i,e,n){if(typeof i=="function"?i===e:i.has(e))return arguments.length<3?e:n;throw new TypeError("Private element is not present on this object")}function XR(i){if(i===void 0)throw new ReferenceError("this hasn't been initialised - super() hasn't been called");return i}function ey(i,e,n){return e=io(e),QR(i,sp()?Reflect.construct(e,n||[],io(i).constructor):e.apply(i,n))}function qR(i,e){if(e.has(i))throw new TypeError("Cannot initialize the same private elements twice on an object")}function ty(i,e){if(!(i instanceof e))throw new TypeError("Cannot call a class as a function")}function Zd(i,e){return i.get(Qv(i,e))}function qv(i,e,n){qR(i,e),e.set(i,n)}function jv(i,e,n){return i.set(Qv(i,e),n),n}function ny(i,e,n){if(sp())return Reflect.construct.apply(null,arguments);var r=[null];r.push.apply(r,e);var s=new(i.bind.apply(i,r));return s}function jR(i,e){for(var n=0;n<e.length;n++){var r=e[n];r.enumerable=r.enumerable||!1,r.configurable=!0,"value"in r&&(r.writable=!0),Object.defineProperty(i,sy(r.key),r)}}function iy(i,e,n){return e&&jR(i.prototype,e),Object.defineProperty(i,"prototype",{writable:!1}),i}function Fu(i,e,n){return(e=sy(e))in i?Object.defineProperty(i,e,{value:n,enumerable:!0,configurable:!0,writable:!0}):i[e]=n,i}function np(){return np=typeof Reflect<"u"&&Reflect.get?Reflect.get.bind():function(i,e,n){var r=eP(i,e);if(r){var s=Object.getOwnPropertyDescriptor(r,e);return s.get?s.get.call(arguments.length<3?i:n):s.value}},np.apply(null,arguments)}function io(i){return io=Object.setPrototypeOf?Object.getPrototypeOf.bind():function(e){return e.__proto__||Object.getPrototypeOf(e)},io(i)}function ry(i,e){if(typeof e!="function"&&e!==null)throw new TypeError("Super expression must either be null or a function");i.prototype=Object.create(e&&e.prototype,{constructor:{value:i,writable:!0,configurable:!0}}),Object.defineProperty(i,"prototype",{writable:!1}),e&&ip(i,e)}function sp(){try{var i=!Boolean.prototype.valueOf.call(Reflect.construct(Boolean,[],function(){}))}catch{}return(sp=function(){return!!i})()}function YR(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function $R(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function ZR(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function KR(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function Yv(i,e){var n=Object.keys(i);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(i);e&&(r=r.filter(function(s){return Object.getOwnPropertyDescriptor(i,s).enumerable})),n.push.apply(n,r)}return n}function JR(i){for(var e=1;e<arguments.length;e++){var n=arguments[e]!=null?arguments[e]:{};e%2?Yv(Object(n),!0).forEach(function(r){Fu(i,r,n[r])}):Object.getOwnPropertyDescriptors?Object.defineProperties(i,Object.getOwnPropertyDescriptors(n)):Yv(Object(n)).forEach(function(r){Object.defineProperty(i,r,Object.getOwnPropertyDescriptor(n,r))})}return i}function QR(i,e){if(e&&(typeof e=="object"||typeof e=="function"))return e;if(e!==void 0)throw new TypeError("Derived constructors may only return object or undefined");return XR(i)}function ip(i,e){return ip=Object.setPrototypeOf?Object.setPrototypeOf.bind():function(n,r){return n.__proto__=r,n},ip(i,e)}function Ea(i,e){return HR(i)||$R(i,e)||oy(i,e)||ZR()}function eP(i,e){for(;!{}.hasOwnProperty.call(i,e)&&(i=io(i))!==null;);return i}function Kd(i,e,n,r){var s=np(io(i.prototype),e,n);return typeof s=="function"?function(o){return s.apply(n,o)}:s}function ri(i){return WR(i)||YR(i)||oy(i)||KR()}function tP(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return(e==="string"?String:Number)(i)}function sy(i){var e=tP(i,"string");return typeof e=="symbol"?e:e+""}function rp(i){"@babel/helpers - typeof";return rp=typeof Symbol=="function"&&typeof Symbol.iterator=="symbol"?function(e){return typeof e}:function(e){return e&&typeof Symbol=="function"&&e.constructor===Symbol&&e!==Symbol.prototype?"symbol":typeof e},rp(i)}function oy(i,e){if(i){if(typeof i=="string")return tp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?tp(i,e):void 0}}var ay=function(e){e instanceof Array?e.forEach(ay):(e.map&&e.map.dispose(),e.dispose())},op=function(e){e.geometry&&e.geometry.dispose(),e.material&&ay(e.material),e.texture&&e.texture.dispose(),e.children&&e.children.forEach(op)},$v=function(e){for(;e.children.length;){var n=e.children[0];e.remove(n),op(n)}},Jd=new WeakMap,Nu=new WeakMap,wa=(function(i){function e(n){var r,s=arguments.length>1&&arguments[1]!==void 0?arguments[1]:{},o=s.dataBindAttr,a=o===void 0?"__data":o,l=s.objBindAttr,c=l===void 0?"__threeObj":l;return ty(this,e),r=ey(this,e),Fu(r,"scene",void 0),qv(r,Jd,void 0),qv(r,Nu,void 0),r.scene=n,jv(Jd,r,a),jv(Nu,r,c),r.onRemoveObj(function(){}),r}return ry(e,i),iy(e,[{key:"onCreateObj",value:function(r){var s=this;return Kd(e,"onCreateObj",this)([function(o){var a=r(o);return o[Zd(Nu,s)]=a,a[Zd(Jd,s)]=o,s.scene.add(a),a}]),this}},{key:"onRemoveObj",value:function(r){var s=this;return Kd(e,"onRemoveObj",this)([function(o,a){var l=Kd(e,"getData",s)([o]);r(o,a),s.scene.remove(o),op(o),delete l[Zd(Nu,s)]}]),this}}])})(gv),Ma=function(e){return isNaN(e)?parseInt(Pe(e).toHex(),16):e},Qd=function(e){return isNaN(e)?Pe(e).getAlpha():1},nP=qr(ba);function Zv(i,e,n){!e||typeof n!="string"||i.filter(function(r){return!r[n]}).forEach(function(r){r[n]=nP(e(r))})}function iP(i,e){var n=i.nodes,r=i.links,s=arguments.length>2&&arguments[2]!==void 0?arguments[2]:{},o=s.nodeFilter,a=o===void 0?function(){return!0}:o,l=s.onLoopError,c=l===void 0?function(p){throw"Invalid DAG structure! Found cycle in node path: ".concat(p.join(" -> "),".")}:l,u={};n.forEach(function(p){return u[e(p)]={data:p,out:[],depth:-1,skip:!a(p)}}),r.forEach(function(p){var g=p.source,v=p.target,_=y(g),m=y(v);if(!u.hasOwnProperty(_))throw"Missing source node with id: ".concat(_);if(!u.hasOwnProperty(m))throw"Missing target node with id: ".concat(m);var w=u[_],T=u[m];w.out.push(T);function y(b){return rp(b)==="object"?e(b):b}});var h=[];f(Object.values(u));var d=Object.assign.apply(Object,[{}].concat(ri(Object.entries(u).filter(function(p){var g=Ea(p,2),v=g[1];return!v.skip}).map(function(p){var g=Ea(p,2),v=g[0],_=g[1];return Fu({},v,_.depth)}))));return d;function f(p){for(var g=arguments.length>1&&arguments[1]!==void 0?arguments[1]:[],v=arguments.length>2&&arguments[2]!==void 0?arguments[2]:0,_=function(){var y=p[m];if(g.indexOf(y)!==-1){var b=[].concat(ri(g.slice(g.indexOf(y))),[y]).map(function(S){return e(S.data)});return h.some(function(S){return S.length===b.length&&S.every(function(A,x){return A===b[x]})})||(h.push(b),c(b)),1}v>y.depth&&(y.depth=v,f(y.out,[].concat(ri(g),[y]),v+(y.skip?0:1)))},m=0,w=p.length;m<w;m++)_()}}var ke=window.THREE?window.THREE:{Group:hi,Mesh:kt,MeshLambertMaterial:Uo,Color:We,BufferGeometry:Ft,BufferAttribute:pn,Matrix4:st,Vector3:F,SphereGeometry:Dr,CylinderGeometry:Ds,TubeGeometry:No,ConeGeometry:Lo,Line:Ro,LineBasicMaterial:Is,QuadraticBezierCurve3:Lr,CubicBezierCurve3:Os,Box3:In},Kv={graph:b_,forcelayout:Jv.default},rP=2,ep=new ke.BufferGeometry().setAttribute?"setAttribute":"addAttribute",Uu=new ke.BufferGeometry().applyMatrix4?"applyMatrix4":"applyMatrix",sP=ei({props:{jsonUrl:{onChange:function(e,n){var r=this;e&&!n.fetchingJson&&(n.fetchingJson=!0,n.onLoading(),fetch(e).then(function(s){return s.json()}).then(function(s){n.fetchingJson=!1,n.onFinishLoading(s),r.graphData(s)}))},triggerUpdate:!1},graphData:{default:{nodes:[],links:[]},onChange:function(e,n){n.engineRunning=!1}},numDimensions:{default:3,onChange:function(e,n){var r=n.d3ForceLayout.force("charge");r&&r.strength(e>2?-60:-30),e<3&&s(n.graphData.nodes,"z"),e<2&&s(n.graphData.nodes,"y");function s(o,a){o.forEach(function(l){delete l[a],delete l["v".concat(a)]})}}},dagMode:{onChange:function(e,n){!e&&n.forceEngine==="d3"&&(n.graphData.nodes||[]).forEach(function(r){return r.fx=r.fy=r.fz=void 0})}},dagLevelDistance:{},dagNodeFilter:{default:function(e){return!0}},onDagError:{triggerUpdate:!1},nodeRelSize:{default:4},nodeId:{default:"id"},nodeVal:{default:"val"},nodeResolution:{default:8},nodeColor:{default:"color"},nodeAutoColorBy:{},nodeOpacity:{default:.75},nodeVisibility:{default:!0},nodeThreeObject:{},nodeThreeObjectExtend:{default:!1},nodePositionUpdate:{triggerUpdate:!1},linkSource:{default:"source"},linkTarget:{default:"target"},linkVisibility:{default:!0},linkColor:{default:"color"},linkAutoColorBy:{},linkOpacity:{default:.2},linkWidth:{},linkResolution:{default:6},linkCurvature:{default:0,triggerUpdate:!1},linkCurveRotation:{default:0,triggerUpdate:!1},linkMaterial:{},linkThreeObject:{},linkThreeObjectExtend:{default:!1},linkPositionUpdate:{triggerUpdate:!1},linkDirectionalArrowLength:{default:0},linkDirectionalArrowColor:{},linkDirectionalArrowRelPos:{default:.5,triggerUpdate:!1},linkDirectionalArrowResolution:{default:8},linkDirectionalParticles:{default:0},linkDirectionalParticleSpeed:{default:.01,triggerUpdate:!1},linkDirectionalParticleOffset:{default:0,triggerUpdate:!1},linkDirectionalParticleWidth:{default:.5},linkDirectionalParticleColor:{},linkDirectionalParticleResolution:{default:4},linkDirectionalParticleThreeObject:{},forceEngine:{default:"d3"},d3AlphaMin:{default:0},d3AlphaDecay:{default:.0228,triggerUpdate:!1,onChange:function(e,n){n.d3ForceLayout.alphaDecay(e)}},d3AlphaTarget:{default:0,triggerUpdate:!1,onChange:function(e,n){n.d3ForceLayout.alphaTarget(e)}},d3VelocityDecay:{default:.4,triggerUpdate:!1,onChange:function(e,n){n.d3ForceLayout.velocityDecay(e)}},ngraphPhysics:{default:{timeStep:20,gravity:-1.2,theta:.8,springLength:30,springCoefficient:8e-4,dragCoefficient:.02}},warmupTicks:{default:0,triggerUpdate:!1},cooldownTicks:{default:1/0,triggerUpdate:!1},cooldownTime:{default:15e3,triggerUpdate:!1},onLoading:{default:function(){},triggerUpdate:!1},onFinishLoading:{default:function(){},triggerUpdate:!1},onUpdate:{default:function(){},triggerUpdate:!1},onFinishUpdate:{default:function(){},triggerUpdate:!1},onEngineTick:{default:function(){},triggerUpdate:!1},onEngineStop:{default:function(){},triggerUpdate:!1}},methods:{refresh:function(e){return e._flushObjects=!0,e._rerender(),this},d3Force:function(e,n,r){return r===void 0?e.d3ForceLayout.force(n):(e.d3ForceLayout.force(n,r),this)},d3ReheatSimulation:function(e){return e.d3ForceLayout.alpha(1),this.resetCountdown(),this},resetCountdown:function(e){return e.cntTicks=0,e.startTickTime=new Date,e.engineRunning=!0,this},tickFrame:function(e){var n=e.forceEngine!=="ngraph";return e.engineRunning&&r(),s(),o(),this;function r(){++e.cntTicks>e.cooldownTicks||new Date-e.startTickTime>e.cooldownTime||n&&e.d3AlphaMin>0&&e.d3ForceLayout.alpha()<e.d3AlphaMin?(e.engineRunning=!1,e.onEngineStop()):(e.layout[n?"tick":"step"](),e.onEngineTick());var a=we(e.nodeThreeObjectExtend);e.nodeDataMapper.entries().forEach(function(f){var p=Ea(f,2),g=p[0],v=p[1];if(v){var _=n?g:e.layout.getNodePosition(g[e.nodeId]),m=a(g);(!e.nodePositionUpdate||!e.nodePositionUpdate(m?v.children[0]:v,{x:_.x,y:_.y,z:_.z},g)||m)&&(v.position.x=_.x,v.position.y=_.y||0,v.position.z=_.z||0)}});var l=we(e.linkWidth),c=we(e.linkCurvature),u=we(e.linkCurveRotation),h=we(e.linkThreeObjectExtend);e.linkDataMapper.entries().forEach(function(f){var p=Ea(f,2),g=p[0],v=p[1];if(v){var _=n?g:e.layout.getLinkPosition(e.layout.graph.getLink(g.source,g.target).id),m=_[n?"source":"from"],w=_[n?"target":"to"];if(!(!m||!w||!m.hasOwnProperty("x")||!w.hasOwnProperty("x"))){d(g);var T=h(g);if(!(e.linkPositionUpdate&&e.linkPositionUpdate(T?v.children[1]:v,{start:{x:m.x,y:m.y,z:m.z},end:{x:w.x,y:w.y,z:w.z}},g)&&!T)){var y=30,b=g.__curve,S=v.children.length?v.children[0]:v;if(S.type==="Line"){if(b){var x=b.getPoints(y);S.geometry.getAttribute("position").array.length!==x.length*3&&S.geometry[ep]("position",new ke.BufferAttribute(new Float32Array(x.length*3),3)),S.geometry.setFromPoints(x)}else{var A=S.geometry.getAttribute("position");(!A||!A.array||A.array.length!==6)&&S.geometry[ep]("position",A=new ke.BufferAttribute(new Float32Array(6),3)),A.array[0]=m.x,A.array[1]=m.y||0,A.array[2]=m.z||0,A.array[3]=w.x,A.array[4]=w.y||0,A.array[5]=w.z||0,A.needsUpdate=!0}S.geometry.computeBoundingSphere()}else if(S.type==="Mesh")if(b){S.geometry.type.match(/^Tube(Buffer)?Geometry$/)||(S.position.set(0,0,0),S.rotation.set(0,0,0),S.scale.set(1,1,1));var I=Math.ceil(l(g)*10)/10,D=I/2,k=new ke.TubeGeometry(b,y,D,e.linkResolution,!1);S.geometry.dispose(),S.geometry=k}else{if(!S.geometry.type.match(/^Cylinder(Buffer)?Geometry$/)){var C=Math.ceil(l(g)*10)/10,L=C/2,P=new ke.CylinderGeometry(L,L,1,e.linkResolution,1,!1);P[Uu](new ke.Matrix4().makeTranslation(0,1/2,0)),P[Uu](new ke.Matrix4().makeRotationX(Math.PI/2)),S.geometry.dispose(),S.geometry=P}var O=new ke.Vector3(m.x,m.y||0,m.z||0),U=new ke.Vector3(w.x,w.y||0,w.z||0),M=O.distanceTo(U);S.position.x=O.x,S.position.y=O.y,S.position.z=O.z,S.scale.z=M,S.parent.localToWorld(U),S.lookAt(U)}}}}});function d(f){var p=n?f:e.layout.getLinkPosition(e.layout.graph.getLink(f.source,f.target).id),g=p[n?"source":"from"],v=p[n?"target":"to"];if(!(!g||!v||!g.hasOwnProperty("x")||!v.hasOwnProperty("x"))){var _=c(f);if(!_)f.__curve=null;else{var m=new ke.Vector3(g.x,g.y||0,g.z||0),w=new ke.Vector3(v.x,v.y||0,v.z||0),T=m.distanceTo(w),y,b=u(f);if(T>0){var S=v.x-g.x,A=v.y-g.y||0,x=new ke.Vector3().subVectors(w,m),C=x.clone().multiplyScalar(_).cross(S!==0||A!==0?new ke.Vector3(0,0,1):new ke.Vector3(0,1,0)).applyAxisAngle(x.normalize(),b).add(new ke.Vector3().addVectors(m,w).divideScalar(2));y=new ke.QuadraticBezierCurve3(m,C,w)}else{var L=_*70,P=-b,O=P+Math.PI/2;y=new ke.CubicBezierCurve3(m,new ke.Vector3(L*Math.cos(O),L*Math.sin(O),0).add(m),new ke.Vector3(L*Math.cos(P),L*Math.sin(P),0).add(m),w)}f.__curve=y}}}}function s(){var a=we(e.linkDirectionalArrowRelPos),l=we(e.linkDirectionalArrowLength),c=we(e.nodeVal);e.arrowDataMapper.entries().forEach(function(u){var h=Ea(u,2),d=h[0],f=h[1];if(f){var p=n?d:e.layout.getLinkPosition(e.layout.graph.getLink(d.source,d.target).id),g=p[n?"source":"from"],v=p[n?"target":"to"];if(!(!g||!v||!g.hasOwnProperty("x")||!v.hasOwnProperty("x"))){var _=Math.cbrt(Math.max(0,c(g)||1))*e.nodeRelSize,m=Math.cbrt(Math.max(0,c(v)||1))*e.nodeRelSize,w=l(d),T=a(d),y=d.__curve?function(L){return d.__curve.getPoint(L)}:function(L){var P=function(U,M,I,D){return M[U]+(I[U]-M[U])*D||0};return{x:P("x",g,v,L),y:P("y",g,v,L),z:P("z",g,v,L)}},b=d.__curve?d.__curve.getLength():Math.sqrt(["x","y","z"].map(function(L){return Math.pow((v[L]||0)-(g[L]||0),2)}).reduce(function(L,P){return L+P},0)),S=_+w+(b-_-m-w)*T,A=y(S/b),x=y((S-w)/b);["x","y","z"].forEach(function(L){return f.position[L]=x[L]});var C=ny(ke.Vector3,ri(["x","y","z"].map(function(L){return A[L]})));f.parent.localToWorld(C),f.lookAt(C)}}})}function o(){var a=we(e.linkDirectionalParticleSpeed),l=we(e.linkDirectionalParticleOffset);e.graphData.links.forEach(function(c){var u=e.particlesDataMapper.getObj(c),h=u&&u.children,d=c.__singleHopPhotonsObj&&c.__singleHopPhotonsObj.children;if(!((!d||!d.length)&&(!h||!h.length))){var f=n?c:e.layout.getLinkPosition(e.layout.graph.getLink(c.source,c.target).id),p=f[n?"source":"from"],g=f[n?"target":"to"];if(!(!p||!g||!p.hasOwnProperty("x")||!g.hasOwnProperty("x"))){var v=a(c),_=Math.abs(l(c)),m=c.__curve?function(T){return c.__curve.getPoint(T)}:function(T){var y=function(S,A,x,C){return A[S]+(x[S]-A[S])*C||0};return{x:y("x",p,g,T),y:y("y",p,g,T),z:y("z",p,g,T)}},w=[].concat(ri(h||[]),ri(d||[]));w.forEach(function(T,y){var b=T.parent.__linkThreeObjType==="singleHopPhotons";if(T.hasOwnProperty("__progressRatio")||(T.__progressRatio=b?v<0?1:0:(y+_)/h.length),T.__progressRatio+=v,T.__progressRatio>=1||T.__progressRatio<0)if(!b)T.__progressRatio=T.__progressRatio%1,T.__progressRatio<0&&T.__progressRatio++;else{T.parent.remove(T),$v(T);return}var S=T.__progressRatio,A=m(S);T.geometry.type!=="SphereGeometry"&&T.lookAt(A.x,A.y,A.z),["x","y","z"].forEach(function(x){return T.position[x]=A[x]})})}}})}},emitParticle:function(e,n){if(n&&e.graphData.links.includes(n)){if(!n.__singleHopPhotonsObj){var r=new ke.Group;r.__linkThreeObjType="singleHopPhotons",n.__singleHopPhotonsObj=r,e.graphScene.add(r)}var s=we(e.linkDirectionalParticleThreeObject)(n);if(s&&e.linkDirectionalParticleThreeObject===s&&(s=s.clone()),!s){var o=we(e.linkDirectionalParticleWidth),a=Math.ceil(o(n)*10)/10/2,l=e.linkDirectionalParticleResolution,c=new ke.SphereGeometry(a,l,l),u=we(e.linkColor),h=we(e.linkDirectionalParticleColor),d=h(n)||u(n)||"#f0f0f0",f=new ke.Color(Ma(d)),p=e.linkOpacity*3,g=new ke.MeshLambertMaterial({color:f,transparent:!0,opacity:p});s=new ke.Mesh(c,g)}n.__singleHopPhotonsObj.add(s)}return this},getGraphBbox:function(e){var n=arguments.length>1&&arguments[1]!==void 0?arguments[1]:function(){return!0};if(!e.initialised)return null;var r=(function s(o){var a=[];if(o.geometry){o.geometry.computeBoundingBox();var l=new ke.Box3;l.copy(o.geometry.boundingBox).applyMatrix4(o.matrixWorld),a.push(l)}return a.concat.apply(a,ri((o.children||[]).filter(function(c){return!c.hasOwnProperty("__graphObjType")||c.__graphObjType==="node"&&n(c.__data)}).map(s)))})(e.graphScene);return r.length?Object.assign.apply(Object,ri(["x","y","z"].map(function(s){return Fu({},s,[Xr(r,function(o){return o.min[s]}),Wr(r,function(o){return o.max[s]})])}))):null}},stateInit:function(){return{d3ForceLayout:fa().force("link",sa()).force("charge",da()).force("center",ta()).force("dagRadial",null).stop(),engineRunning:!1}},init:function(e,n){n.graphScene=e,n.nodeDataMapper=new wa(e,{objBindAttr:"__threeObj"}),n.linkDataMapper=new wa(e,{objBindAttr:"__lineObj"}),n.arrowDataMapper=new wa(e,{objBindAttr:"__arrowObj"}),n.particlesDataMapper=new wa(e,{objBindAttr:"__photonsObj"})},update:function(e,n){var r=function(oe){return oe.some(function(V){return n.hasOwnProperty(V)})};if(e.engineRunning=!1,typeof e.onUpdate=="function"&&e.onUpdate(),e.nodeAutoColorBy!==null&&r(["nodeAutoColorBy","graphData","nodeColor"])&&Zv(e.graphData.nodes,we(e.nodeAutoColorBy),e.nodeColor),e.linkAutoColorBy!==null&&r(["linkAutoColorBy","graphData","linkColor"])&&Zv(e.graphData.links,we(e.linkAutoColorBy),e.linkColor),e._flushObjects||r(["graphData","nodeThreeObject","nodeThreeObjectExtend","nodeVal","nodeColor","nodeVisibility","nodeRelSize","nodeResolution","nodeOpacity"])){var s=we(e.nodeThreeObject),o=we(e.nodeThreeObjectExtend),a=we(e.nodeVal),l=we(e.nodeColor),c=we(e.nodeVisibility),u={},h={};(e._flushObjects||r(["nodeThreeObject","nodeThreeObjectExtend"]))&&e.nodeDataMapper.clear(),e.nodeDataMapper.onCreateObj(function(ee){var oe=s(ee),V=o(ee);oe&&e.nodeThreeObject===oe&&(oe=oe.clone());var Z;return oe&&!V?Z=oe:(Z=new ke.Mesh,Z.__graphDefaultObj=!0,oe&&V&&Z.add(oe)),Z.__graphObjType="node",Z}).onUpdateObj(function(ee,oe){if(ee.__graphDefaultObj){var V=a(oe)||1,Z=Math.cbrt(V)*e.nodeRelSize,ce=e.nodeResolution;(!ee.geometry.type.match(/^Sphere(Buffer)?Geometry$/)||ee.geometry.parameters.radius!==Z||ee.geometry.parameters.widthSegments!==ce)&&(u.hasOwnProperty(V)||(u[V]=new ke.SphereGeometry(Z,ce,ce)),ee.geometry.dispose(),ee.geometry=u[V]);var Ee=l(oe),ue=new ke.Color(Ma(Ee||"#ffffaa")),Be=e.nodeOpacity*Qd(Ee);(ee.material.type!=="MeshLambertMaterial"||!ee.material.color.equals(ue)||ee.material.opacity!==Be)&&(h.hasOwnProperty(Ee)||(h[Ee]=new ke.MeshLambertMaterial({color:ue,transparent:!0,opacity:Be})),ee.material.dispose(),ee.material=h[Ee])}}).digest(e.graphData.nodes.filter(c))}if(e._flushObjects||r(["graphData","linkThreeObject","linkThreeObjectExtend","linkMaterial","linkColor","linkWidth","linkVisibility","linkResolution","linkOpacity","linkDirectionalArrowLength","linkDirectionalArrowColor","linkDirectionalArrowResolution","linkDirectionalParticles","linkDirectionalParticleWidth","linkDirectionalParticleColor","linkDirectionalParticleResolution","linkDirectionalParticleThreeObject"])){var d=we(e.linkThreeObject),f=we(e.linkThreeObjectExtend),p=we(e.linkMaterial),g=we(e.linkVisibility),v=we(e.linkColor),_=we(e.linkWidth),m={},w={},T={},y=e.graphData.links.filter(g);if((e._flushObjects||r(["linkThreeObject","linkThreeObjectExtend","linkWidth"]))&&e.linkDataMapper.clear(),e.linkDataMapper.onRemoveObj(function(ee){var oe=ee.__data&&ee.__data.__singleHopPhotonsObj;oe&&(oe.parent.remove(oe),$v(oe),delete ee.__data.__singleHopPhotonsObj)}).onCreateObj(function(ee){var oe=d(ee),V=f(ee);oe&&e.linkThreeObject===oe&&(oe=oe.clone());var Z;if(!oe||V){var ce=!!_(ee);if(ce)Z=new ke.Mesh;else{var Ee=new ke.BufferGeometry;Ee[ep]("position",new ke.BufferAttribute(new Float32Array(6),3)),Z=new ke.Line(Ee)}}var ue;return oe?V?(ue=new ke.Group,ue.__graphDefaultObj=!0,ue.add(Z),ue.add(oe)):ue=oe:(ue=Z,ue.__graphDefaultObj=!0),ue.renderOrder=10,ue.__graphObjType="link",ue}).onUpdateObj(function(ee,oe){if(ee.__graphDefaultObj){var V=ee.children.length?ee.children[0]:ee,Z=Math.ceil(_(oe)*10)/10,ce=!!Z;if(ce){var Ee=Z/2,ue=e.linkResolution;if(!V.geometry.type.match(/^Cylinder(Buffer)?Geometry$/)||V.geometry.parameters.radiusTop!==Ee||V.geometry.parameters.radialSegments!==ue){if(!m.hasOwnProperty(Z)){var Be=new ke.CylinderGeometry(Ee,Ee,1,ue,1,!1);Be[Uu](new ke.Matrix4().makeTranslation(0,1/2,0)),Be[Uu](new ke.Matrix4().makeRotationX(Math.PI/2)),m[Z]=Be}V.geometry.dispose(),V.geometry=m[Z]}}var ut=p(oe);if(ut)V.material=ut;else{var Ve=v(oe),$e=new ke.Color(Ma(Ve||"#f0f0f0")),Je=e.linkOpacity*Qd(Ve),Ge=ce?"MeshLambertMaterial":"LineBasicMaterial";if(V.material.type!==Ge||!V.material.color.equals($e)||V.material.opacity!==Je){var tt=ce?w:T;tt.hasOwnProperty(Ve)||(tt[Ve]=new ke[Ge]({color:$e,transparent:Je<1,opacity:Je,depthWrite:Je>=1})),V.material.dispose(),V.material=tt[Ve]}}}}).digest(y),e.linkDirectionalArrowLength||n.hasOwnProperty("linkDirectionalArrowLength")){var b=we(e.linkDirectionalArrowLength),S=we(e.linkDirectionalArrowColor);e.arrowDataMapper.onCreateObj(function(){var ee=new ke.Mesh(void 0,new ke.MeshLambertMaterial({transparent:!0}));return ee.__linkThreeObjType="arrow",ee}).onUpdateObj(function(ee,oe){var V=b(oe),Z=e.linkDirectionalArrowResolution;if(!ee.geometry.type.match(/^Cone(Buffer)?Geometry$/)||ee.geometry.parameters.height!==V||ee.geometry.parameters.radialSegments!==Z){var ce=new ke.ConeGeometry(V*.25,V,Z);ce.translate(0,V/2,0),ce.rotateX(Math.PI/2),ee.geometry.dispose(),ee.geometry=ce}var Ee=S(oe)||v(oe)||"#f0f0f0";ee.material.color=new ke.Color(Ma(Ee)),ee.material.opacity=e.linkOpacity*3*Qd(Ee)}).digest(y.filter(b))}if(e.linkDirectionalParticles||n.hasOwnProperty("linkDirectionalParticles")){var A=we(e.linkDirectionalParticles),x=we(e.linkDirectionalParticleWidth),C=we(e.linkDirectionalParticleColor),L=we(e.linkDirectionalParticleThreeObject),P={},O={};e.particlesDataMapper.onCreateObj(function(){var ee=new ke.Group;return ee.__linkThreeObjType="photons",ee.__photonDataMapper=new wa(ee),ee}).onUpdateObj(function(ee,oe){var V=!!ee.children.length&&ee.children[0],Z=L(oe),ce,Ee;if(Z)ce=Z.geometry,Ee=Z.material;else{var ue=Math.ceil(x(oe)*10)/10/2,Be=e.linkDirectionalParticleResolution;V&&V.geometry.parameters.radius===ue&&V.geometry.parameters.widthSegments===Be?ce=V.geometry:(O.hasOwnProperty(ue)||(O[ue]=new ke.SphereGeometry(ue,Be,Be)),ce=O[ue]);var ut=C(oe)||v(oe)||"#f0f0f0",Ve=new ke.Color(Ma(ut)),$e=e.linkOpacity*3;V&&V.material.color.equals(Ve)&&V.material.opacity===$e?Ee=V.material:(P.hasOwnProperty(ut)||(P[ut]=new ke.MeshLambertMaterial({color:Ve,transparent:!0,opacity:$e})),Ee=P[ut])}V&&(V.geometry!==ce&&V.geometry.dispose(),V.material!==Ee&&V.material.dispose());var Je=Math.round(Math.abs(A(oe)));ee.__photonDataMapper.id(function(Ge){return Ge.idx}).onCreateObj(function(){return new ke.Mesh(ce,Ee)}).onUpdateObj(function(Ge){Ge.geometry=ce,Ge.material=Ee}).digest(ri(new Array(Je)).map(function(Ge,tt){return{idx:tt}}))}).digest(y.filter(A))}}if(e._flushObjects=!1,r(["graphData","nodeId","linkSource","linkTarget","numDimensions","forceEngine","dagMode","dagNodeFilter","dagLevelDistance"])){e.engineRunning=!1,e.graphData.links.forEach(function(ee){ee.source=ee[e.linkSource],ee.target=ee[e.linkTarget]});var U=e.forceEngine!=="ngraph",M;if(U){(M=e.d3ForceLayout).stop().alpha(1).numDimensions(e.numDimensions).nodes(e.graphData.nodes);var I=e.d3ForceLayout.force("link");I&&I.id(function(ee){return ee[e.nodeId]}).links(e.graphData.links);var D=e.dagMode&&iP(e.graphData,function(ee){return ee[e.nodeId]},{nodeFilter:e.dagNodeFilter,onLoopError:e.onDagError||void 0}),k=Math.max.apply(Math,ri(Object.values(D||[]))),q=e.dagLevelDistance||e.graphData.nodes.length/(k||1)*rP*(["radialin","radialout"].indexOf(e.dagMode)!==-1?.7:1);if(["lr","rl","td","bu","zin","zout"].includes(n.dagMode)){var W=["lr","rl"].includes(n.dagMode)?"fx":["td","bu"].includes(n.dagMode)?"fy":"fz";e.graphData.nodes.filter(e.dagNodeFilter).forEach(function(ee){return delete ee[W]})}if(["lr","rl","td","bu","zin","zout"].includes(e.dagMode)){var j=["rl","td","zout"].includes(e.dagMode),B=function(oe){return(D[oe[e.nodeId]]-k/2)*q*(j?-1:1)},Q=["lr","rl"].includes(e.dagMode)?"fx":["td","bu"].includes(e.dagMode)?"fy":"fz";e.graphData.nodes.filter(e.dagNodeFilter).forEach(function(ee){return ee[Q]=B(ee)})}e.d3ForceLayout.force("dagRadial",["radialin","radialout"].indexOf(e.dagMode)!==-1?pa(function(ee){var oe=D[ee[e.nodeId]]||-1;return(e.dagMode==="radialin"?k-oe:oe)*q}).strength(function(ee){return e.dagNodeFilter(ee)?1:0}):null)}else{var re=Kv.graph();e.graphData.nodes.forEach(function(ee){re.addNode(ee[e.nodeId])}),e.graphData.links.forEach(function(ee){re.addLink(ee.source,ee.target)}),M=Kv.forcelayout(re,JR({dimensions:e.numDimensions},e.ngraphPhysics)),M.graph=re}for(var be=0;be<e.warmupTicks&&!(U&&e.d3AlphaMin>0&&e.d3ForceLayout.alpha()<e.d3AlphaMin);be++)M[U?"tick":"step"]();e.layout=M,this.resetCountdown()}e.engineRunning=!0,e.onFinishUpdate()}});function oP(i){var e=arguments.length>1&&arguments[1]!==void 0?arguments[1]:Object,n=arguments.length>2&&arguments[2]!==void 0?arguments[2]:!1,r=(function(s){function o(){var a;ty(this,o);for(var l=arguments.length,c=new Array(l),u=0;u<l;u++)c[u]=arguments[u];return a=ey(this,o,[].concat(c)),a.__kapsuleInstance=ny(i,[].concat(ri(n?[a]:[]),c)),a}return ry(o,s),iy(o)})(e);return Object.keys(i()).forEach(function(s){return r.prototype[s]=function(){var o,a=(o=this.__kapsuleInstance)[s].apply(o,arguments);return a===this.__kapsuleInstance?this:a}}),r}var aP=window.THREE?window.THREE:{Group:hi},ap=oP(sP,aP.Group,!0);var ku=class{constructor(){throw new Error("WebGPU renderer is not bundled in this viewer")}};var lp={type:"change"},hp={type:"start"},fp={type:"end"},Bu=1e-6,et={NONE:-1,ROTATE:0,ZOOM:1,PAN:2,TOUCH_ROTATE:3,TOUCH_ZOOM_PAN:4},zu=new fe,fr=new fe,lP=new F,Vu=new F,cp=new F,dr=new Ut,Gu=new F,Hu=new F,up=new F,Wu=new F,Xu=class extends $n{constructor(e,n=null){super(e,n),this.screen={left:0,top:0,width:0,height:0},this.rotateSpeed=1,this.zoomSpeed=1.2,this.panSpeed=.3,this.rollSpeed=1,this.noRotate=!1,this.noZoom=!1,this.noPan=!1,this.multiTouchRoll=!1,this.staticMoving=!1,this.dynamicDampingFactor=.2,this.minDistance=0,this.maxDistance=1/0,this.minZoom=0,this.maxZoom=1/0,this.keys=["KeyA","KeyS","KeyD"],this.mouseButtons={LEFT:Et.ROTATE,MIDDLE:Et.DOLLY,RIGHT:Et.PAN},this.target=new F,this.state=et.NONE,this.keyState=et.NONE,this._lastPosition=new F,this._lastUp=new F,this._lastZoom=1,this._touchZoomDistanceStart=0,this._touchZoomDistanceEnd=0,this._touchRollAngle=0,this._touchRollPointerIds="",this._lastAngle=0,this._lastRollAngle=0,this._eye=new F,this._movePrev=new fe,this._moveCurr=new fe,this._lastAxis=new F,this._zoomStart=new fe,this._zoomEnd=new fe,this._panStart=new fe,this._panEnd=new fe,this._pointers=[],this._pointerPositions={},this._onPointerMove=uP.bind(this),this._onPointerDown=cP.bind(this),this._onPointerUp=hP.bind(this),this._onPointerCancel=fP.bind(this),this._onContextMenu=yP.bind(this),this._onMouseWheel=vP.bind(this),this._onKeyDown=pP.bind(this),this._onKeyUp=dP.bind(this),this._onTouchStart=xP.bind(this),this._onTouchMove=bP.bind(this),this._onTouchEnd=SP.bind(this),this._onMouseDown=mP.bind(this),this._onMouseMove=gP.bind(this),this._onMouseUp=_P.bind(this),this._target0=this.target.clone(),this._position0=this.object.position.clone(),this._up0=this.object.up.clone(),this._zoom0=this.object.zoom,n!==null&&(this.connect(n),this.handleResize()),this.update()}connect(e){super.connect(e),window.addEventListener("keydown",this._onKeyDown),window.addEventListener("keyup",this._onKeyUp),this.domElement.addEventListener("pointerdown",this._onPointerDown),this.domElement.addEventListener("pointercancel",this._onPointerCancel),this.domElement.addEventListener("wheel",this._onMouseWheel,{passive:!1}),this.domElement.addEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction="none"}disconnect(){window.removeEventListener("keydown",this._onKeyDown),window.removeEventListener("keyup",this._onKeyUp),this.domElement.removeEventListener("pointerdown",this._onPointerDown),this.domElement.ownerDocument.removeEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.removeEventListener("pointerup",this._onPointerUp),this.domElement.removeEventListener("pointercancel",this._onPointerCancel),this.domElement.removeEventListener("wheel",this._onMouseWheel),this.domElement.removeEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction=""}dispose(){this.disconnect()}handleResize(){let e=this.domElement.getBoundingClientRect(),n=this.domElement.ownerDocument.documentElement;this.screen.left=e.left+window.pageXOffset-n.clientLeft,this.screen.top=e.top+window.pageYOffset-n.clientTop,this.screen.width=e.width,this.screen.height=e.height}update(){if(this._eye.subVectors(this.object.position,this.target),this.noRotate||(this._rotateCamera(),this.multiTouchRoll===!0&&this._rollCamera()),this.noZoom||this._zoomCamera(),this.noPan||this._panCamera(),this.object.position.addVectors(this.target,this._eye),this.object.isPerspectiveCamera){this._checkDistances(),this.object.lookAt(this.target);let e=this._lastPosition.distanceToSquared(this.object.position)>Bu,n=this._lastUp.distanceToSquared(this.object.up)>Bu;(e||n)&&(this.dispatchEvent(lp),this._lastPosition.copy(this.object.position),this._lastUp.copy(this.object.up))}else if(this.object.isOrthographicCamera){this.object.lookAt(this.target);let e=this._lastPosition.distanceToSquared(this.object.position)>Bu,n=this._lastUp.distanceToSquared(this.object.up)>Bu,r=this._lastZoom!==this.object.zoom;(e||n||r)&&(this.dispatchEvent(lp),this._lastPosition.copy(this.object.position),this._lastUp.copy(this.object.up),this._lastZoom=this.object.zoom)}else console.warn("THREE.TrackballControls: Unsupported camera type.")}reset(){this.state=et.NONE,this.keyState=et.NONE,this.target.copy(this._target0),this.object.position.copy(this._position0),this.object.up.copy(this._up0),this.object.zoom=this._zoom0,this.object.updateProjectionMatrix(),this._eye.subVectors(this.object.position,this.target),this.object.lookAt(this.target),this.dispatchEvent(lp),this._lastPosition.copy(this.object.position),this._lastUp.copy(this.object.up),this._lastZoom=this.object.zoom}_panCamera(){if(fr.copy(this._panEnd).sub(this._panStart),fr.lengthSq()){if(this.object.isOrthographicCamera){let e=(this.object.right-this.object.left)/this.object.zoom/this.domElement.clientWidth,n=(this.object.top-this.object.bottom)/this.object.zoom/this.domElement.clientWidth;fr.x*=e,fr.y*=n}fr.multiplyScalar(this._eye.length()*this.panSpeed),Vu.copy(this._eye).cross(this.object.up).setLength(fr.x),Vu.add(lP.copy(this.object.up).setLength(fr.y)),this.object.position.add(Vu),this.target.add(Vu),this.staticMoving?this._panStart.copy(this._panEnd):this._panStart.add(fr.subVectors(this._panEnd,this._panStart).multiplyScalar(this.dynamicDampingFactor))}}_rotateCamera(){Wu.set(this._moveCurr.x-this._movePrev.x,this._moveCurr.y-this._movePrev.y,0);let e=Wu.length();e?(this._eye.copy(this.object.position).sub(this.target),Gu.copy(this._eye).normalize(),Hu.copy(this.object.up).normalize(),up.crossVectors(Hu,Gu).normalize(),Hu.setLength(this._moveCurr.y-this._movePrev.y),up.setLength(this._moveCurr.x-this._movePrev.x),Wu.copy(Hu.add(up)),cp.crossVectors(Wu,this._eye).normalize(),e*=this.rotateSpeed,dr.setFromAxisAngle(cp,e),this._eye.applyQuaternion(dr),this.object.up.applyQuaternion(dr),this._lastAxis.copy(cp),this._lastAngle=e):!this.staticMoving&&this._lastAngle&&(this._lastAngle*=Math.sqrt(1-this.dynamicDampingFactor),this._eye.copy(this.object.position).sub(this.target),dr.setFromAxisAngle(this._lastAxis,this._lastAngle),this._eye.applyQuaternion(dr),this.object.up.applyQuaternion(dr)),this._movePrev.copy(this._moveCurr)}_rollCamera(){let e=this._getTouchRollAngle();if(e)this._lastRollAngle=e*this.rollSpeed;else if(!this.staticMoving&&this._lastRollAngle)this._lastRollAngle*=Math.sqrt(1-this.dynamicDampingFactor);else return;Gu.copy(this._eye).normalize(),dr.setFromAxisAngle(Gu,this._lastRollAngle),this.object.up.applyQuaternion(dr)}_getTouchRollAngle(){let e=this._pointers[0],n=this._pointers[1];if(n===void 0||e.pointerType!=="touch"||n.pointerType!=="touch")return this._touchRollPointerIds="",0;let r=`${e.pointerId},${n.pointerId}`,s=this._pointerPositions[e.pointerId],o=this._pointerPositions[n.pointerId],a=Math.atan2(o.y-s.y,o.x-s.x);if(r!==this._touchRollPointerIds)return this._touchRollPointerIds=r,this._touchRollAngle=a,0;let l=a-this._touchRollAngle;return this._touchRollAngle=a,l>Math.PI?l-=2*Math.PI:l<-Math.PI&&(l+=2*Math.PI),l}_zoomCamera(){let e;this.state===et.TOUCH_ZOOM_PAN?(e=this._touchZoomDistanceStart/this._touchZoomDistanceEnd,this._touchZoomDistanceStart=this._touchZoomDistanceEnd,this.object.isPerspectiveCamera?this._eye.multiplyScalar(e):this.object.isOrthographicCamera?(this.object.zoom=Gs.clamp(this.object.zoom/e,this.minZoom,this.maxZoom),this._lastZoom!==this.object.zoom&&this.object.updateProjectionMatrix()):console.warn("THREE.TrackballControls: Unsupported camera type")):(e=1+(this._zoomEnd.y-this._zoomStart.y)*this.zoomSpeed,e!==1&&e>0&&(this.object.isPerspectiveCamera?this._eye.multiplyScalar(e):this.object.isOrthographicCamera?(this.object.zoom=Gs.clamp(this.object.zoom/e,this.minZoom,this.maxZoom),this._lastZoom!==this.object.zoom&&this.object.updateProjectionMatrix()):console.warn("THREE.TrackballControls: Unsupported camera type")),this.staticMoving?this._zoomStart.copy(this._zoomEnd):this._zoomStart.y+=(this._zoomEnd.y-this._zoomStart.y)*this.dynamicDampingFactor)}_getMouseOnScreen(e,n){return zu.set((e-this.screen.left)/this.screen.width,(n-this.screen.top)/this.screen.height),zu}_getMouseOnCircle(e,n){return zu.set((e-this.screen.width*.5-this.screen.left)/(this.screen.width*.5),(this.screen.height+2*(this.screen.top-n))/this.screen.width),zu}_addPointer(e){this._pointers.push(e)}_removePointer(e){delete this._pointerPositions[e.pointerId];for(let n=0;n<this._pointers.length;n++)if(this._pointers[n].pointerId==e.pointerId){this._pointers.splice(n,1);return}}_trackPointer(e){let n=this._pointerPositions[e.pointerId];n===void 0&&(n=new fe,this._pointerPositions[e.pointerId]=n),n.set(e.pageX,e.pageY)}_getSecondPointerPosition(e){let n=e.pointerId===this._pointers[0].pointerId?this._pointers[1]:this._pointers[0];return this._pointerPositions[n.pointerId]}_checkDistances(){(!this.noZoom||!this.noPan)&&(this._eye.lengthSq()>this.maxDistance*this.maxDistance&&(this.object.position.addVectors(this.target,this._eye.setLength(this.maxDistance)),this._zoomStart.copy(this._zoomEnd)),this._eye.lengthSq()<this.minDistance*this.minDistance&&(this.object.position.addVectors(this.target,this._eye.setLength(this.minDistance)),this._zoomStart.copy(this._zoomEnd)))}};function cP(i){this.enabled!==!1&&(this._pointers.length===0&&(this.domElement.setPointerCapture(i.pointerId),this.domElement.ownerDocument.addEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.addEventListener("pointerup",this._onPointerUp)),this._addPointer(i),i.pointerType==="touch"?this._onTouchStart(i):this._onMouseDown(i))}function uP(i){this.enabled!==!1&&(i.pointerType==="touch"?this._onTouchMove(i):this._onMouseMove(i))}function hP(i){this.enabled!==!1&&(i.pointerType==="touch"?this._onTouchEnd(i):this._onMouseUp(),this._removePointer(i),this._pointers.length===0&&(this.domElement.releasePointerCapture(i.pointerId),this.domElement.ownerDocument.removeEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.removeEventListener("pointerup",this._onPointerUp)))}function fP(i){this._removePointer(i)}function dP(){this.enabled!==!1&&(this.keyState=et.NONE,window.addEventListener("keydown",this._onKeyDown))}function pP(i){this.enabled!==!1&&(window.removeEventListener("keydown",this._onKeyDown),this.keyState===et.NONE&&(i.code===this.keys[et.ROTATE]&&!this.noRotate?this.keyState=et.ROTATE:i.code===this.keys[et.ZOOM]&&!this.noZoom?this.keyState=et.ZOOM:i.code===this.keys[et.PAN]&&!this.noPan&&(this.keyState=et.PAN)))}function mP(i){let e;switch(i.button){case 0:e=this.mouseButtons.LEFT;break;case 1:e=this.mouseButtons.MIDDLE;break;case 2:e=this.mouseButtons.RIGHT;break;default:e=-1}switch(e){case Et.DOLLY:this.state=et.ZOOM;break;case Et.ROTATE:this.state=et.ROTATE;break;case Et.PAN:this.state=et.PAN;break;default:this.state=et.NONE}let n=this.keyState!==et.NONE?this.keyState:this.state;n===et.ROTATE&&!this.noRotate?(this._moveCurr.copy(this._getMouseOnCircle(i.pageX,i.pageY)),this._movePrev.copy(this._moveCurr)):n===et.ZOOM&&!this.noZoom?(this._zoomStart.copy(this._getMouseOnScreen(i.pageX,i.pageY)),this._zoomEnd.copy(this._zoomStart)):n===et.PAN&&!this.noPan&&(this._panStart.copy(this._getMouseOnScreen(i.pageX,i.pageY)),this._panEnd.copy(this._panStart)),this.dispatchEvent(hp)}function gP(i){let e=this.keyState!==et.NONE?this.keyState:this.state;e===et.ROTATE&&!this.noRotate?this._moveCurr.copy(this._getMouseOnCircle(i.pageX,i.pageY)):e===et.ZOOM&&!this.noZoom?this._zoomEnd.copy(this._getMouseOnScreen(i.pageX,i.pageY)):e===et.PAN&&!this.noPan&&this._panEnd.copy(this._getMouseOnScreen(i.pageX,i.pageY))}function _P(){this.state=et.NONE,this.dispatchEvent(fp)}function vP(i){if(this.enabled!==!1&&this.noZoom!==!0){switch(i.preventDefault(),i.deltaMode){case 2:this._zoomStart.y-=i.deltaY*.025;break;case 1:this._zoomStart.y-=i.deltaY*.01;break;default:this._zoomStart.y-=i.deltaY*25e-5;break}this.dispatchEvent(hp),this.dispatchEvent(fp)}}function yP(i){this.enabled!==!1&&i.preventDefault()}function xP(i){if(this._trackPointer(i),this._pointers.length===1)this.state=et.TOUCH_ROTATE,this._moveCurr.copy(this._getMouseOnCircle(this._pointers[0].pageX,this._pointers[0].pageY)),this._movePrev.copy(this._moveCurr);else{this.state=et.TOUCH_ZOOM_PAN;let e=this._pointers[0].pageX-this._pointers[1].pageX,n=this._pointers[0].pageY-this._pointers[1].pageY;this._touchZoomDistanceEnd=this._touchZoomDistanceStart=Math.sqrt(e*e+n*n);let r=(this._pointers[0].pageX+this._pointers[1].pageX)/2,s=(this._pointers[0].pageY+this._pointers[1].pageY)/2;this._panStart.copy(this._getMouseOnScreen(r,s)),this._panEnd.copy(this._panStart)}this.dispatchEvent(hp)}function bP(i){if(this._trackPointer(i),this._pointers.length===1)this._moveCurr.copy(this._getMouseOnCircle(i.pageX,i.pageY));else{let e=this._getSecondPointerPosition(i),n=i.pageX-e.x,r=i.pageY-e.y;this._touchZoomDistanceEnd=Math.sqrt(n*n+r*r);let s=(i.pageX+e.x)/2,o=(i.pageY+e.y)/2;this._panEnd.copy(this._getMouseOnScreen(s,o))}}function SP(i){switch(this._pointers.length){case 0:this.state=et.NONE;break;case 1:this.state=et.TOUCH_ROTATE,this._moveCurr.copy(this._getMouseOnCircle(i.pageX,i.pageY)),this._movePrev.copy(this._moveCurr);break;case 2:this.state=et.TOUCH_ZOOM_PAN;for(let e=0;e<this._pointers.length;e++)if(this._pointers[e].pointerId!==i.pointerId){let n=this._pointerPositions[this._pointers[e].pointerId];this._moveCurr.copy(this._getMouseOnCircle(n.x,n.y)),this._movePrev.copy(this._moveCurr);break}break}this.dispatchEvent(fp)}var ly={type:"change"},pp={type:"start"},uy={type:"end"},qu=new Ki,cy=new an,wP=Math.cos(70*Gs.DEG2RAD),It=new F,vn=2*Math.PI,lt={NONE:-1,ROTATE:0,DOLLY:1,PAN:2,TOUCH_ROTATE:3,TOUCH_PAN:4,TOUCH_DOLLY_PAN:5,TOUCH_DOLLY_ROTATE:6},dp=1e-6,ju=class extends $n{constructor(e,n=null){super(e,n),this.state=lt.NONE,this.target=new F,this.cursor=new F,this.minDistance=0,this.maxDistance=1/0,this.minZoom=0,this.maxZoom=1/0,this.minTargetRadius=0,this.maxTargetRadius=1/0,this.minPolarAngle=0,this.maxPolarAngle=Math.PI,this.minAzimuthAngle=-1/0,this.maxAzimuthAngle=1/0,this.enableDamping=!1,this.dampingFactor=.05,this.enableZoom=!0,this.zoomSpeed=1,this.enableRotate=!0,this.rotateSpeed=1,this.keyRotateSpeed=1,this.enablePan=!0,this.panSpeed=1,this.screenSpacePanning=!0,this.keyPanSpeed=7,this.zoomToCursor=!1,this.autoRotate=!1,this.autoRotateSpeed=2,this.keys={LEFT:"ArrowLeft",UP:"ArrowUp",RIGHT:"ArrowRight",BOTTOM:"ArrowDown"},this.mouseButtons={LEFT:Et.ROTATE,MIDDLE:Et.DOLLY,RIGHT:Et.PAN},this.touches={ONE:Dn.ROTATE,TWO:Dn.DOLLY_PAN},this.target0=this.target.clone(),this.position0=this.object.position.clone(),this.zoom0=this.object.zoom,this._cursorStyle="auto",this._domElementKeyEvents=null,this._lastPosition=new F,this._lastQuaternion=new Ut,this._lastTargetPosition=new F,this._quat=new Ut().setFromUnitVectors(e.up,new F(0,1,0)),this._quatInverse=this._quat.clone().invert(),this._spherical=new Us,this._sphericalDelta=new Us,this._scale=1,this._panOffset=new F,this._rotateStart=new fe,this._rotateEnd=new fe,this._rotateDelta=new fe,this._panStart=new fe,this._panEnd=new fe,this._panDelta=new fe,this._dollyStart=new fe,this._dollyEnd=new fe,this._dollyDelta=new fe,this._dollyDirection=new F,this._mouse=new fe,this._performCursorZoom=!1,this._pointers=[],this._pointerPositions={},this._controlActive=!1,this._onPointerMove=EP.bind(this),this._onPointerDown=MP.bind(this),this._onPointerUp=AP.bind(this),this._onContextMenu=DP.bind(this),this._onMouseWheel=RP.bind(this),this._onKeyDown=PP.bind(this),this._onTouchStart=IP.bind(this),this._onTouchMove=LP.bind(this),this._onMouseDown=TP.bind(this),this._onMouseMove=CP.bind(this),this._interceptControlDown=OP.bind(this),this._interceptControlUp=NP.bind(this),this.domElement!==null&&this.connect(this.domElement),this.update()}set cursorStyle(e){this._cursorStyle=e,e==="grab"?this.domElement.style.cursor="grab":this.domElement.style.cursor="auto"}get cursorStyle(){return this._cursorStyle}connect(e){super.connect(e),this.domElement.addEventListener("pointerdown",this._onPointerDown),this.domElement.addEventListener("pointercancel",this._onPointerUp),this.domElement.addEventListener("contextmenu",this._onContextMenu),this.domElement.addEventListener("wheel",this._onMouseWheel,{passive:!1}),this.domElement.getRootNode().addEventListener("keydown",this._interceptControlDown,{passive:!0,capture:!0}),this.domElement.style.touchAction="none"}disconnect(){this.state=lt.NONE,this.domElement.removeEventListener("pointerdown",this._onPointerDown),this.domElement.ownerDocument.removeEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.removeEventListener("pointerup",this._onPointerUp),this.domElement.removeEventListener("pointercancel",this._onPointerUp),this.domElement.removeEventListener("wheel",this._onMouseWheel),this.domElement.removeEventListener("contextmenu",this._onContextMenu),this.stopListenToKeyEvents();let e=this.domElement.getRootNode();e.removeEventListener("keydown",this._interceptControlDown,{capture:!0}),e.removeEventListener("keyup",this._interceptControlUp,{capture:!0}),this._controlActive=!1,this._pointers.length=0,this._pointerPositions={},this.domElement.style.touchAction="",this.domElement.style.cursor="auto"}dispose(){this.disconnect()}getPolarAngle(){return this._spherical.phi}getAzimuthalAngle(){return this._spherical.theta}getDistance(){return this.object.position.distanceTo(this.target)}listenToKeyEvents(e){e.addEventListener("keydown",this._onKeyDown),this._domElementKeyEvents=e}stopListenToKeyEvents(){this._domElementKeyEvents!==null&&(this._domElementKeyEvents.removeEventListener("keydown",this._onKeyDown),this._domElementKeyEvents=null)}saveState(){this.target0.copy(this.target),this.position0.copy(this.object.position),this.zoom0=this.object.zoom}reset(){this.target.copy(this.target0),this.object.position.copy(this.position0),this.object.zoom=this.zoom0,this.object.updateProjectionMatrix(),this.dispatchEvent(ly),this.update(),this.state=lt.NONE}pan(e,n){this._pan(e,n),this.update()}dollyIn(e){this._dollyIn(e),this.update()}dollyOut(e){this._dollyOut(e),this.update()}rotateLeft(e){this._rotateLeft(e),this.update()}rotateUp(e){this._rotateUp(e),this.update()}update(e=null){let n=this.object.position;It.copy(n).sub(this.target),It.applyQuaternion(this._quat),this._spherical.setFromVector3(It),this.autoRotate&&this.state===lt.NONE&&this._rotateLeft(this._getAutoRotationAngle(e)),this.enableDamping?(this._spherical.theta+=this._sphericalDelta.theta*this.dampingFactor,this._spherical.phi+=this._sphericalDelta.phi*this.dampingFactor):(this._spherical.theta+=this._sphericalDelta.theta,this._spherical.phi+=this._sphericalDelta.phi);let r=this.minAzimuthAngle,s=this.maxAzimuthAngle;isFinite(r)&&isFinite(s)&&(r<-Math.PI?r+=vn:r>Math.PI&&(r-=vn),s<-Math.PI?s+=vn:s>Math.PI&&(s-=vn),r<=s?this._spherical.theta=Math.max(r,Math.min(s,this._spherical.theta)):this._spherical.theta=this._spherical.theta>(r+s)/2?Math.max(r,this._spherical.theta):Math.min(s,this._spherical.theta)),this._spherical.phi=Math.max(this.minPolarAngle,Math.min(this.maxPolarAngle,this._spherical.phi)),this._spherical.makeSafe(),this.enableDamping===!0?this.target.addScaledVector(this._panOffset,this.dampingFactor):this.target.add(this._panOffset),this.target.sub(this.cursor),this.target.clampLength(this.minTargetRadius,this.maxTargetRadius),this.target.add(this.cursor);let o=!1;if(this.zoomToCursor&&this._performCursorZoom||this.object.isOrthographicCamera)this._spherical.radius=this._clampDistance(this._spherical.radius);else{let a=this._spherical.radius;this._spherical.radius=this._clampDistance(this._spherical.radius*this._scale),o=a!=this._spherical.radius}if(It.setFromSpherical(this._spherical),It.applyQuaternion(this._quatInverse),n.copy(this.target).add(It),this.object.lookAt(this.target),this.enableDamping===!0?(this._sphericalDelta.theta*=1-this.dampingFactor,this._sphericalDelta.phi*=1-this.dampingFactor,this._panOffset.multiplyScalar(1-this.dampingFactor)):(this._sphericalDelta.set(0,0,0),this._panOffset.set(0,0,0)),this.zoomToCursor&&this._performCursorZoom){let a=null;if(this.object.isPerspectiveCamera){let l=It.length();a=this._clampDistance(l*this._scale);let c=l-a;this.object.position.addScaledVector(this._dollyDirection,c),this.object.updateMatrixWorld(),o=!!c}else if(this.object.isOrthographicCamera){let l=new F(this._mouse.x,this._mouse.y,0);l.unproject(this.object);let c=this.object.zoom;this.object.zoom=Math.max(this.minZoom,Math.min(this.maxZoom,this.object.zoom/this._scale)),this.object.updateProjectionMatrix(),o=c!==this.object.zoom;let u=new F(this._mouse.x,this._mouse.y,0);u.unproject(this.object),this.object.position.sub(u).add(l),this.object.updateMatrixWorld(),a=It.length()}else console.warn("WARNING: OrbitControls.js encountered an unknown camera type - zoom to cursor disabled."),this.zoomToCursor=!1;a!==null&&(this.screenSpacePanning?this.target.set(0,0,-1).transformDirection(this.object.matrix).multiplyScalar(a).add(this.object.position):(qu.origin.copy(this.object.position),qu.direction.set(0,0,-1).transformDirection(this.object.matrix),Math.abs(this.object.up.dot(qu.direction))<wP?this.object.lookAt(this.target):(cy.setFromNormalAndCoplanarPoint(this.object.up,this.target),qu.intersectPlane(cy,this.target))))}else if(this.object.isOrthographicCamera){let a=this.object.zoom;this.object.zoom=Math.max(this.minZoom,Math.min(this.maxZoom,this.object.zoom/this._scale)),a!==this.object.zoom&&(this.object.updateProjectionMatrix(),o=!0)}return this._scale=1,this._performCursorZoom=!1,o||this._lastPosition.distanceToSquared(this.object.position)>dp||8*(1-this._lastQuaternion.dot(this.object.quaternion))>dp||this._lastTargetPosition.distanceToSquared(this.target)>dp?(this.dispatchEvent(ly),this._lastPosition.copy(this.object.position),this._lastQuaternion.copy(this.object.quaternion),this._lastTargetPosition.copy(this.target),!0):!1}_getAutoRotationAngle(e){return e!==null?vn/60*this.autoRotateSpeed*e:vn/60/60*this.autoRotateSpeed}_getZoomScale(e){let n=Math.abs(e*.01);return Math.pow(.95,this.zoomSpeed*n)}_rotateLeft(e){this._sphericalDelta.theta-=e}_rotateUp(e){this._sphericalDelta.phi-=e}_panLeft(e,n){It.setFromMatrixColumn(n,0),It.multiplyScalar(-e),this._panOffset.add(It)}_panUp(e,n){this.screenSpacePanning===!0?It.setFromMatrixColumn(n,1):(It.setFromMatrixColumn(n,0),It.crossVectors(this.object.up,It)),It.multiplyScalar(e),this._panOffset.add(It)}_pan(e,n){let r=this.domElement;if(this.object.isPerspectiveCamera){let s=this.object.position;It.copy(s).sub(this.target);let o=It.length();o*=Math.tan(this.object.fov/2*Math.PI/180),this._panLeft(2*e*o/r.clientHeight,this.object.matrix),this._panUp(2*n*o/r.clientHeight,this.object.matrix)}else this.object.isOrthographicCamera?(this._panLeft(e*(this.object.right-this.object.left)/this.object.zoom/r.clientWidth,this.object.matrix),this._panUp(n*(this.object.top-this.object.bottom)/this.object.zoom/r.clientHeight,this.object.matrix)):(console.warn("WARNING: OrbitControls.js encountered an unknown camera type - pan disabled."),this.enablePan=!1)}_dollyOut(e){this.object.isPerspectiveCamera||this.object.isOrthographicCamera?this._scale/=e:(console.warn("WARNING: OrbitControls.js encountered an unknown camera type - dolly/zoom disabled."),this.enableZoom=!1)}_dollyIn(e){this.object.isPerspectiveCamera||this.object.isOrthographicCamera?this._scale*=e:(console.warn("WARNING: OrbitControls.js encountered an unknown camera type - dolly/zoom disabled."),this.enableZoom=!1)}_updateZoomParameters(e,n){if(!this.zoomToCursor)return;this._performCursorZoom=!0;let r=this.domElement.getBoundingClientRect(),s=e-r.left,o=n-r.top,a=r.width,l=r.height;this._mouse.x=s/a*2-1,this._mouse.y=-(o/l)*2+1,this._dollyDirection.set(this._mouse.x,this._mouse.y,1).unproject(this.object).sub(this.object.position).normalize()}_clampDistance(e){return Math.max(this.minDistance,Math.min(this.maxDistance,e))}_handleMouseDownRotate(e){this._rotateStart.set(e.clientX,e.clientY)}_handleMouseDownDolly(e){this._updateZoomParameters(e.clientX,e.clientX),this._dollyStart.set(e.clientX,e.clientY)}_handleMouseDownPan(e){this._panStart.set(e.clientX,e.clientY)}_handleMouseMoveRotate(e){this._rotateEnd.set(e.clientX,e.clientY),this._rotateDelta.subVectors(this._rotateEnd,this._rotateStart).multiplyScalar(this.rotateSpeed);let n=this.domElement;this._rotateLeft(vn*this._rotateDelta.x/n.clientHeight),this._rotateUp(vn*this._rotateDelta.y/n.clientHeight),this._rotateStart.copy(this._rotateEnd),this.update()}_handleMouseMoveDolly(e){this._dollyEnd.set(e.clientX,e.clientY),this._dollyDelta.subVectors(this._dollyEnd,this._dollyStart),this._dollyDelta.y>0?this._dollyOut(this._getZoomScale(this._dollyDelta.y)):this._dollyDelta.y<0&&this._dollyIn(this._getZoomScale(this._dollyDelta.y)),this._dollyStart.copy(this._dollyEnd),this.update()}_handleMouseMovePan(e){this._panEnd.set(e.clientX,e.clientY),this._panDelta.subVectors(this._panEnd,this._panStart).multiplyScalar(this.panSpeed),this._pan(this._panDelta.x,this._panDelta.y),this._panStart.copy(this._panEnd),this.update()}_handleMouseWheel(e){this._updateZoomParameters(e.clientX,e.clientY),e.deltaY<0?this._dollyIn(this._getZoomScale(e.deltaY)):e.deltaY>0&&this._dollyOut(this._getZoomScale(e.deltaY)),this.update()}_handleKeyDown(e){let n=!1;switch(e.code){case this.keys.UP:e.ctrlKey||e.metaKey||e.shiftKey?this.enableRotate&&this._rotateUp(vn*this.keyRotateSpeed/this.domElement.clientHeight):this.enablePan&&this._pan(0,this.keyPanSpeed),n=!0;break;case this.keys.BOTTOM:e.ctrlKey||e.metaKey||e.shiftKey?this.enableRotate&&this._rotateUp(-vn*this.keyRotateSpeed/this.domElement.clientHeight):this.enablePan&&this._pan(0,-this.keyPanSpeed),n=!0;break;case this.keys.LEFT:e.ctrlKey||e.metaKey||e.shiftKey?this.enableRotate&&this._rotateLeft(vn*this.keyRotateSpeed/this.domElement.clientHeight):this.enablePan&&this._pan(this.keyPanSpeed,0),n=!0;break;case this.keys.RIGHT:e.ctrlKey||e.metaKey||e.shiftKey?this.enableRotate&&this._rotateLeft(-vn*this.keyRotateSpeed/this.domElement.clientHeight):this.enablePan&&this._pan(-this.keyPanSpeed,0),n=!0;break}n&&(e.preventDefault(),this.update())}_handleTouchStartRotate(e){if(this._pointers.length===1)this._rotateStart.set(e.pageX,e.pageY);else{let n=this._getSecondPointerPosition(e),r=.5*(e.pageX+n.x),s=.5*(e.pageY+n.y);this._rotateStart.set(r,s)}}_handleTouchStartPan(e){if(this._pointers.length===1)this._panStart.set(e.pageX,e.pageY);else{let n=this._getSecondPointerPosition(e),r=.5*(e.pageX+n.x),s=.5*(e.pageY+n.y);this._panStart.set(r,s)}}_handleTouchStartDolly(e){let n=this._getSecondPointerPosition(e),r=e.pageX-n.x,s=e.pageY-n.y,o=Math.sqrt(r*r+s*s);this._dollyStart.set(0,o)}_handleTouchStartDollyPan(e){this.enableZoom&&this._handleTouchStartDolly(e),this.enablePan&&this._handleTouchStartPan(e)}_handleTouchStartDollyRotate(e){this.enableZoom&&this._handleTouchStartDolly(e),this.enableRotate&&this._handleTouchStartRotate(e)}_handleTouchMoveRotate(e){if(this._pointers.length==1)this._rotateEnd.set(e.pageX,e.pageY);else{let r=this._getSecondPointerPosition(e),s=.5*(e.pageX+r.x),o=.5*(e.pageY+r.y);this._rotateEnd.set(s,o)}this._rotateDelta.subVectors(this._rotateEnd,this._rotateStart).multiplyScalar(this.rotateSpeed);let n=this.domElement;this._rotateLeft(vn*this._rotateDelta.x/n.clientHeight),this._rotateUp(vn*this._rotateDelta.y/n.clientHeight),this._rotateStart.copy(this._rotateEnd)}_handleTouchMovePan(e){if(this._pointers.length===1)this._panEnd.set(e.pageX,e.pageY);else{let n=this._getSecondPointerPosition(e),r=.5*(e.pageX+n.x),s=.5*(e.pageY+n.y);this._panEnd.set(r,s)}this._panDelta.subVectors(this._panEnd,this._panStart).multiplyScalar(this.panSpeed),this._pan(this._panDelta.x,this._panDelta.y),this._panStart.copy(this._panEnd)}_handleTouchMoveDolly(e){let n=this._getSecondPointerPosition(e),r=e.pageX-n.x,s=e.pageY-n.y,o=Math.sqrt(r*r+s*s);this._dollyEnd.set(0,o),this._dollyDelta.set(0,Math.pow(this._dollyEnd.y/this._dollyStart.y,this.zoomSpeed)),this._dollyOut(this._dollyDelta.y),this._dollyStart.copy(this._dollyEnd);let a=(e.pageX+n.x)*.5,l=(e.pageY+n.y)*.5;this._updateZoomParameters(a,l)}_handleTouchMoveDollyPan(e){this.enableZoom&&this._handleTouchMoveDolly(e),this.enablePan&&this._handleTouchMovePan(e)}_handleTouchMoveDollyRotate(e){this.enableZoom&&this._handleTouchMoveDolly(e),this.enableRotate&&this._handleTouchMoveRotate(e)}_addPointer(e){this._pointers.push(e.pointerId)}_removePointer(e){delete this._pointerPositions[e.pointerId];for(let n=0;n<this._pointers.length;n++)if(this._pointers[n]==e.pointerId){this._pointers.splice(n,1);return}}_isTrackingPointer(e){for(let n=0;n<this._pointers.length;n++)if(this._pointers[n]==e.pointerId)return!0;return!1}_trackPointer(e){let n=this._pointerPositions[e.pointerId];n===void 0&&(n=new fe,this._pointerPositions[e.pointerId]=n),n.set(e.pageX,e.pageY)}_getSecondPointerPosition(e){let n=e.pointerId===this._pointers[0]?this._pointers[1]:this._pointers[0];return this._pointerPositions[n]}_customWheelEvent(e){let n=e.deltaMode,r={clientX:e.clientX,clientY:e.clientY,deltaY:e.deltaY};switch(n){case 1:r.deltaY*=16;break;case 2:r.deltaY*=100;break}return e.ctrlKey&&!this._controlActive&&(r.deltaY*=10),r}};function MP(i){this.enabled!==!1&&(this._pointers.length===0&&(this.domElement.setPointerCapture(i.pointerId),this.domElement.ownerDocument.addEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.addEventListener("pointerup",this._onPointerUp)),!this._isTrackingPointer(i)&&(this._addPointer(i),i.pointerType==="touch"?this._onTouchStart(i):this._onMouseDown(i),this._cursorStyle==="grab"&&(this.domElement.style.cursor="grabbing")))}function EP(i){this.enabled!==!1&&(i.pointerType==="touch"?this._onTouchMove(i):this._onMouseMove(i))}function AP(i){switch(this._removePointer(i),this._pointers.length){case 0:this.domElement.releasePointerCapture(i.pointerId),this.domElement.ownerDocument.removeEventListener("pointermove",this._onPointerMove),this.domElement.ownerDocument.removeEventListener("pointerup",this._onPointerUp),this.dispatchEvent(uy),this.state=lt.NONE,this._cursorStyle==="grab"&&(this.domElement.style.cursor="grab");break;case 1:let e=this._pointers[0],n=this._pointerPositions[e];this._onTouchStart({pointerId:e,pageX:n.x,pageY:n.y});break}}function TP(i){let e;switch(i.button){case 0:e=this.mouseButtons.LEFT;break;case 1:e=this.mouseButtons.MIDDLE;break;case 2:e=this.mouseButtons.RIGHT;break;default:e=-1}switch(e){case Et.DOLLY:if(this.enableZoom===!1)return;this._handleMouseDownDolly(i),this.state=lt.DOLLY;break;case Et.ROTATE:if(i.ctrlKey||i.metaKey||i.shiftKey){if(this.enablePan===!1)return;this._handleMouseDownPan(i),this.state=lt.PAN}else{if(this.enableRotate===!1)return;this._handleMouseDownRotate(i),this.state=lt.ROTATE}break;case Et.PAN:if(i.ctrlKey||i.metaKey||i.shiftKey){if(this.enableRotate===!1)return;this._handleMouseDownRotate(i),this.state=lt.ROTATE}else{if(this.enablePan===!1)return;this._handleMouseDownPan(i),this.state=lt.PAN}break;default:this.state=lt.NONE}this.state!==lt.NONE&&this.dispatchEvent(pp)}function CP(i){switch(this.state){case lt.ROTATE:if(this.enableRotate===!1)return;this._handleMouseMoveRotate(i);break;case lt.DOLLY:if(this.enableZoom===!1)return;this._handleMouseMoveDolly(i);break;case lt.PAN:if(this.enablePan===!1)return;this._handleMouseMovePan(i);break}}function RP(i){this.enabled===!1||this.enableZoom===!1||this.state!==lt.NONE||(i.preventDefault(),this.dispatchEvent(pp),this._handleMouseWheel(this._customWheelEvent(i)),this.dispatchEvent(uy))}function PP(i){this.enabled!==!1&&this._handleKeyDown(i)}function IP(i){switch(this._trackPointer(i),this._pointers.length){case 1:switch(this.touches.ONE){case Dn.ROTATE:if(this.enableRotate===!1)return;this._handleTouchStartRotate(i),this.state=lt.TOUCH_ROTATE;break;case Dn.PAN:if(this.enablePan===!1)return;this._handleTouchStartPan(i),this.state=lt.TOUCH_PAN;break;default:this.state=lt.NONE}break;case 2:switch(this.touches.TWO){case Dn.DOLLY_PAN:if(this.enableZoom===!1&&this.enablePan===!1)return;this._handleTouchStartDollyPan(i),this.state=lt.TOUCH_DOLLY_PAN;break;case Dn.DOLLY_ROTATE:if(this.enableZoom===!1&&this.enableRotate===!1)return;this._handleTouchStartDollyRotate(i),this.state=lt.TOUCH_DOLLY_ROTATE;break;default:this.state=lt.NONE}break;default:this.state=lt.NONE}this.state!==lt.NONE&&this.dispatchEvent(pp)}function LP(i){switch(this._trackPointer(i),this.state){case lt.TOUCH_ROTATE:if(this.enableRotate===!1)return;this._handleTouchMoveRotate(i),this.update();break;case lt.TOUCH_PAN:if(this.enablePan===!1)return;this._handleTouchMovePan(i),this.update();break;case lt.TOUCH_DOLLY_PAN:if(this.enableZoom===!1&&this.enablePan===!1)return;this._handleTouchMoveDollyPan(i),this.update();break;case lt.TOUCH_DOLLY_ROTATE:if(this.enableZoom===!1&&this.enableRotate===!1)return;this._handleTouchMoveDollyRotate(i),this.update();break;default:this.state=lt.NONE}}function DP(i){this.enabled!==!1&&i.preventDefault()}function OP(i){i.key==="Control"&&(this._controlActive=!0,this.domElement.getRootNode().addEventListener("keyup",this._interceptControlUp,{passive:!0,capture:!0}))}function NP(i){i.key==="Control"&&(this._controlActive=!1,this.domElement.getRootNode().removeEventListener("keyup",this._interceptControlUp,{passive:!0,capture:!0}))}var UP={type:"change"},hy=1e-6,fy=new Ut,Yu=class extends $n{constructor(e,n=null){super(e,n),this.movementSpeed=1,this.rollSpeed=.005,this.dragToLook=!1,this.autoForward=!1,this._moveState={up:0,down:0,left:0,right:0,forward:0,back:0,pitchUp:0,pitchDown:0,yawLeft:0,yawRight:0,rollLeft:0,rollRight:0},this._moveVector=new F(0,0,0),this._rotationVector=new F(0,0,0),this._lastQuaternion=new Ut,this._lastPosition=new F,this._status=0,this._onKeyDown=FP.bind(this),this._onKeyUp=kP.bind(this),this._onPointerMove=zP.bind(this),this._onPointerDown=BP.bind(this),this._onPointerUp=VP.bind(this),this._onPointerCancel=GP.bind(this),this._onContextMenu=HP.bind(this),n!==null&&this.connect(n)}connect(e){super.connect(e),window.addEventListener("keydown",this._onKeyDown),window.addEventListener("keyup",this._onKeyUp),this.domElement.addEventListener("pointermove",this._onPointerMove),this.domElement.addEventListener("pointerdown",this._onPointerDown),this.domElement.addEventListener("pointerup",this._onPointerUp),this.domElement.addEventListener("pointercancel",this._onPointerCancel),this.domElement.addEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction="none"}disconnect(){window.removeEventListener("keydown",this._onKeyDown),window.removeEventListener("keyup",this._onKeyUp),this.domElement.removeEventListener("pointermove",this._onPointerMove),this.domElement.removeEventListener("pointerdown",this._onPointerDown),this.domElement.removeEventListener("pointerup",this._onPointerUp),this.domElement.removeEventListener("pointercancel",this._onPointerCancel),this.domElement.removeEventListener("contextmenu",this._onContextMenu),this.domElement.style.touchAction=""}dispose(){this.disconnect()}update(e){if(this.enabled===!1)return;let n=this.object,r=e*this.movementSpeed,s=e*this.rollSpeed;n.translateX(this._moveVector.x*r),n.translateY(this._moveVector.y*r),n.translateZ(this._moveVector.z*r),fy.set(this._rotationVector.x*s,this._rotationVector.y*s,this._rotationVector.z*s,1).normalize(),n.quaternion.multiply(fy),(this._lastPosition.distanceToSquared(n.position)>hy||8*(1-this._lastQuaternion.dot(n.quaternion))>hy)&&(this.dispatchEvent(UP),this._lastQuaternion.copy(n.quaternion),this._lastPosition.copy(n.position))}_updateMovementVector(){let e=this._moveState.forward||this.autoForward&&!this._moveState.back?1:0;this._moveVector.x=-this._moveState.left+this._moveState.right,this._moveVector.y=-this._moveState.down+this._moveState.up,this._moveVector.z=-e+this._moveState.back}_updateRotationVector(){this._rotationVector.x=-this._moveState.pitchDown+this._moveState.pitchUp,this._rotationVector.y=-this._moveState.yawRight+this._moveState.yawLeft,this._rotationVector.z=-this._moveState.rollRight+this._moveState.rollLeft}_getContainerDimensions(){return this.domElement!=document?{size:[this.domElement.offsetWidth,this.domElement.offsetHeight],offset:[this.domElement.offsetLeft,this.domElement.offsetTop]}:{size:[window.innerWidth,window.innerHeight],offset:[0,0]}}};function FP(i){if(!(i.altKey||this.enabled===!1)){switch(i.code){case"ShiftLeft":case"ShiftRight":this.movementSpeedMultiplier=.1;break;case"KeyW":this._moveState.forward=1;break;case"KeyS":this._moveState.back=1;break;case"KeyA":this._moveState.left=1;break;case"KeyD":this._moveState.right=1;break;case"KeyR":this._moveState.up=1;break;case"KeyF":this._moveState.down=1;break;case"ArrowUp":this._moveState.pitchUp=1;break;case"ArrowDown":this._moveState.pitchDown=1;break;case"ArrowLeft":this._moveState.yawLeft=1;break;case"ArrowRight":this._moveState.yawRight=1;break;case"KeyQ":this._moveState.rollLeft=1;break;case"KeyE":this._moveState.rollRight=1;break}this._updateMovementVector(),this._updateRotationVector()}}function kP(i){if(this.enabled!==!1){switch(i.code){case"ShiftLeft":case"ShiftRight":this.movementSpeedMultiplier=1;break;case"KeyW":this._moveState.forward=0;break;case"KeyS":this._moveState.back=0;break;case"KeyA":this._moveState.left=0;break;case"KeyD":this._moveState.right=0;break;case"KeyR":this._moveState.up=0;break;case"KeyF":this._moveState.down=0;break;case"ArrowUp":this._moveState.pitchUp=0;break;case"ArrowDown":this._moveState.pitchDown=0;break;case"ArrowLeft":this._moveState.yawLeft=0;break;case"ArrowRight":this._moveState.yawRight=0;break;case"KeyQ":this._moveState.rollLeft=0;break;case"KeyE":this._moveState.rollRight=0;break}this._updateMovementVector(),this._updateRotationVector()}}function BP(i){if(this.enabled!==!1)if(this.dragToLook)this._status++;else{switch(i.button){case 0:this._moveState.forward=1;break;case 2:this._moveState.back=1;break}this._updateMovementVector()}}function zP(i){if(this.enabled!==!1&&(!this.dragToLook||this._status>0)){let e=this._getContainerDimensions(),n=e.size[0]/2,r=e.size[1]/2;this._moveState.yawLeft=-(i.pageX-e.offset[0]-n)/n,this._moveState.pitchDown=(i.pageY-e.offset[1]-r)/r,this._updateRotationVector()}}function VP(i){if(this.enabled!==!1){if(this.dragToLook)this._status--,this._moveState.yawLeft=this._moveState.pitchDown=0;else{switch(i.button){case 0:this._moveState.forward=0;break;case 2:this._moveState.back=0;break}this._updateMovementVector()}this._updateRotationVector()}}function GP(){this.enabled!==!1&&(this.dragToLook?(this._status=0,this._moveState.yawLeft=this._moveState.pitchDown=0):(this._moveState.forward=0,this._moveState.back=0,this._updateMovementVector()),this._updateRotationVector())}function HP(i){this.enabled!==!1&&i.preventDefault()}var dy={name:"CopyShader",uniforms:{tDiffuse:{value:null},opacity:{value:1}},vertexShader:`

		varying vec2 vUv;

		void main() {

			vUv = uv;
			gl_Position = projectionMatrix * modelViewMatrix * vec4( position, 1.0 );

		}`,fragmentShader:`

		uniform float opacity;

		uniform sampler2D tDiffuse;

		varying vec2 vUv;

		void main() {

			vec4 texel = texture2D( tDiffuse, vUv );
			gl_FragColor = opacity * texel;


		}`};var Ni=class{constructor(){this.isPass=!0,this.enabled=!0,this.needsSwap=!0,this.clear=!1,this.renderToScreen=!1}setSize(){}render(){console.error("THREE.Pass: .render() must be implemented in derived pass.")}dispose(){}},WP=new nr(-1,1,1,-1,0,1),mp=class extends Ft{constructor(){super(),this.setAttribute("position",new _t([-1,3,0,-1,-1,0,3,-1,0],3)),this.setAttribute("uv",new _t([0,2,0,0,2,0],2))}},XP=new mp,$u=class{constructor(e){this._mesh=new kt(XP,e)}dispose(){this._mesh.geometry.dispose()}render(e){e.render(this._mesh,WP)}get material(){return this._mesh.material}set material(e){this._mesh.material=e}};var Zu=class extends Ni{constructor(e,n="tDiffuse"){super(),this.textureID=n,this.uniforms=null,this.material=null,e instanceof Jt?(this.uniforms=e.uniforms,this.material=e):e&&(this.uniforms=eu.clone(e.uniforms),this.material=new Jt({name:e.name!==void 0?e.name:"unspecified",defines:Object.assign({},e.defines),uniforms:this.uniforms,vertexShader:e.vertexShader,fragmentShader:e.fragmentShader})),this._fsQuad=new $u(this.material)}render(e,n,r){this.uniforms[this.textureID]&&(this.uniforms[this.textureID].value=r.texture),this._fsQuad.material=this.material,this.renderToScreen?(e.setRenderTarget(null),this._fsQuad.render(e)):(e.setRenderTarget(n),this.clear&&e.clear(e.autoClearColor,e.autoClearDepth,e.autoClearStencil),this._fsQuad.render(e))}dispose(){this.material.dispose(),this._fsQuad.dispose()}};var Aa=class extends Ni{constructor(e,n){super(),this.scene=e,this.camera=n,this.clear=!0,this.needsSwap=!1,this.inverse=!1}render(e,n,r){let s=e.getContext(),o=e.state;o.buffers.color.setMask(!1),o.buffers.depth.setMask(!1),o.buffers.color.setLocked(!0),o.buffers.depth.setLocked(!0);let a,l;this.inverse?(a=0,l=1):(a=1,l=0),o.buffers.stencil.setTest(!0),o.buffers.stencil.setOp(s.REPLACE,s.REPLACE,s.REPLACE),o.buffers.stencil.setFunc(s.ALWAYS,a,4294967295),o.buffers.stencil.setClear(l),o.buffers.stencil.setLocked(!0),e.setRenderTarget(r),this.clear&&e.clear(),e.render(this.scene,this.camera),e.setRenderTarget(n),this.clear&&e.clear(),e.render(this.scene,this.camera),o.buffers.color.setLocked(!1),o.buffers.depth.setLocked(!1),o.buffers.color.setMask(!0),o.buffers.depth.setMask(!0),o.buffers.stencil.setLocked(!1),o.buffers.stencil.setFunc(s.EQUAL,1,4294967295),o.buffers.stencil.setOp(s.KEEP,s.KEEP,s.KEEP),o.buffers.stencil.setLocked(!0)}},Ku=class extends Ni{constructor(){super(),this.needsSwap=!1}render(e){e.state.buffers.stencil.setLocked(!1),e.state.buffers.stencil.setTest(!1)}};var Ju=class{constructor(e,n){if(this.renderer=e,this._pixelRatio=e.getPixelRatio(),n===void 0){let r=e.getSize(new fe);this._width=r.width,this._height=r.height,n=new Zt(this._width*this._pixelRatio,this._height*this._pixelRatio,{type:En}),n.texture.name="EffectComposer.rt1"}else this._width=n.width,this._height=n.height;this.renderTarget1=n,this.renderTarget2=n.clone(),this.renderTarget2.texture.name="EffectComposer.rt2",this.writeBuffer=this.renderTarget1,this.readBuffer=this.renderTarget2,this.renderToScreen=!0,this.passes=[],this.copyPass=new Zu(dy),this.copyPass.material.blending=On,this.timer=new Or}swapBuffers(){let e=this.readBuffer;this.readBuffer=this.writeBuffer,this.writeBuffer=e}addPass(e){this.passes.push(e),e.setSize(this._width*this._pixelRatio,this._height*this._pixelRatio)}insertPass(e,n){this.passes.splice(n,0,e),e.setSize(this._width*this._pixelRatio,this._height*this._pixelRatio)}removePass(e){let n=this.passes.indexOf(e);n!==-1&&this.passes.splice(n,1)}isLastEnabledPass(e){for(let n=e+1;n<this.passes.length;n++)if(this.passes[n].enabled)return!1;return!0}render(e){this.timer.update(),e===void 0&&(e=this.timer.getDelta());let n=this.renderer.getRenderTarget(),r=!1;for(let s=0,o=this.passes.length;s<o;s++){let a=this.passes[s];if(a.enabled!==!1){if(a.renderToScreen=this.renderToScreen&&this.isLastEnabledPass(s),a.render(this.renderer,this.writeBuffer,this.readBuffer,e,r),a.needsSwap){if(r){let l=this.renderer.getContext(),c=this.renderer.state.buffers.stencil;c.setFunc(l.NOTEQUAL,1,4294967295),this.copyPass.render(this.renderer,this.writeBuffer,this.readBuffer,e),c.setFunc(l.EQUAL,1,4294967295)}this.swapBuffers()}Aa!==void 0&&(a instanceof Aa?r=!0:a instanceof Ku&&(r=!1))}}this.renderer.setRenderTarget(n)}reset(e){if(e===void 0){let n=this.renderer.getSize(new fe);this._pixelRatio=this.renderer.getPixelRatio(),this._width=n.width,this._height=n.height,e=this.renderTarget1.clone(),e.setSize(this._width*this._pixelRatio,this._height*this._pixelRatio)}this.renderTarget1.dispose(),this.renderTarget2.dispose(),this.renderTarget1=e,this.renderTarget2=e.clone(),this.writeBuffer=this.renderTarget1,this.readBuffer=this.renderTarget2}setSize(e,n){this._width=e,this._height=n;let r=this._width*this._pixelRatio,s=this._height*this._pixelRatio;this.renderTarget1.setSize(r,s),this.renderTarget2.setSize(r,s);for(let o=0;o<this.passes.length;o++)this.passes[o].setSize(r,s)}setPixelRatio(e){this._pixelRatio=e,this.setSize(this._width,this._height)}dispose(){this.renderTarget1.dispose(),this.renderTarget2.dispose(),this.copyPass.dispose()}};var Qu=class extends Ni{constructor(e,n,r=null,s=null,o=null){super(),this.scene=e,this.camera=n,this.overrideMaterial=r,this.clearColor=s,this.clearAlpha=o,this.clear=!0,this.clearDepth=!1,this.needsSwap=!1,this.isRenderPass=!0,this._oldClearColor=new We}render(e,n,r){let s=e.autoClear;e.autoClear=!1;let o,a;this.overrideMaterial!==null&&(a=this.scene.overrideMaterial,this.scene.overrideMaterial=this.overrideMaterial),this.clearColor!==null&&(e.getClearColor(this._oldClearColor),e.setClearColor(this.clearColor,e.getClearAlpha())),this.clearAlpha!==null&&(o=e.getClearAlpha(),e.setClearAlpha(this.clearAlpha)),this.clearDepth==!0&&e.clearDepth(),e.setRenderTarget(this.renderToScreen?null:r),this.clear===!0&&e.clear(e.autoClearColor,e.autoClearDepth,e.autoClearStencil),e.render(this.scene,this.camera),this.clearColor!==null&&e.setClearColor(this._oldClearColor),this.clearAlpha!==null&&e.setClearAlpha(o),this.overrideMaterial!==null&&(this.scene.overrideMaterial=a),e.autoClear=s}};function un(){return un=Object.assign?Object.assign.bind():function(i){for(var e=1;e<arguments.length;e++){var n=arguments[e];for(var r in n)({}).hasOwnProperty.call(n,r)&&(i[r]=n[r])}return i},un.apply(null,arguments)}function py(i){if(i===void 0)throw new ReferenceError("this hasn't been initialised - super() hasn't been called");return i}function pr(i,e){return pr=Object.setPrototypeOf?Object.setPrototypeOf.bind():function(n,r){return n.__proto__=r,n},pr(i,e)}function my(i,e){i.prototype=Object.create(e.prototype),i.prototype.constructor=i,pr(i,e)}function eh(i){return eh=Object.setPrototypeOf?Object.getPrototypeOf.bind():function(e){return e.__proto__||Object.getPrototypeOf(e)},eh(i)}function gy(i){try{return Function.toString.call(i).indexOf("[native code]")!==-1}catch{return typeof i=="function"}}function gp(){try{var i=!Boolean.prototype.valueOf.call(Reflect.construct(Boolean,[],function(){}))}catch{}return(gp=function(){return!!i})()}function _y(i,e,n){if(gp())return Reflect.construct.apply(null,arguments);var r=[null];r.push.apply(r,e);var s=new(i.bind.apply(i,r));return n&&pr(s,n.prototype),s}function th(i){var e=typeof Map=="function"?new Map:void 0;return th=function(r){if(r===null||!gy(r))return r;if(typeof r!="function")throw new TypeError("Super expression must either be null or a function");if(e!==void 0){if(e.has(r))return e.get(r);e.set(r,s)}function s(){return _y(r,arguments,eh(this).constructor)}return s.prototype=Object.create(r.prototype,{constructor:{value:s,enumerable:!1,writable:!0,configurable:!0}}),pr(s,r)},th(i)}var yn=(function(i){my(e,i);function e(n){var r;if(1)r=i.call(this,"An error occurred. See https://github.com/styled-components/polished/blob/main/src/internalHelpers/errors.md#"+n+" for more information.")||this;else for(var s,o,a;a<s;a++);return py(r)}return e})(th(Error));function vy(i,e){return i.substr(-e.length)===e}var qP=/^([+-]?(?:\d+|\d*\.\d+))([a-z]*|%)$/;function yy(i){if(typeof i!="string")return i;var e=i.match(qP);return e?parseFloat(i):i}var jP=function(e){return function(n,r){r===void 0&&(r="16px");var s=n,o=r;if(typeof n=="string"){if(!vy(n,"px"))throw new yn(69,e,n);s=yy(n)}if(typeof r=="string"){if(!vy(r,"px"))throw new yn(70,e,r);o=yy(r)}if(typeof s=="string")throw new yn(71,n,e);if(typeof o=="string")throw new yn(72,r,e);return""+s/o+e}},by=jP,u4=by("em");var h4=by("rem");function _p(i){return Math.round(i*255)}function YP(i,e,n){return _p(i)+","+_p(e)+","+_p(n)}function Ta(i,e,n,r){if(r===void 0&&(r=YP),e===0)return r(n,n,n);var s=(i%360+360)%360/60,o=(1-Math.abs(2*n-1))*e,a=o*(1-Math.abs(s%2-1)),l=0,c=0,u=0;s>=0&&s<1?(l=o,c=a):s>=1&&s<2?(l=a,c=o):s>=2&&s<3?(c=o,u=a):s>=3&&s<4?(c=a,u=o):s>=4&&s<5?(l=a,u=o):s>=5&&s<6&&(l=o,u=a);var h=n-o/2,d=l+h,f=c+h,p=u+h;return r(d,f,p)}var xy={aliceblue:"f0f8ff",antiquewhite:"faebd7",aqua:"00ffff",aquamarine:"7fffd4",azure:"f0ffff",beige:"f5f5dc",bisque:"ffe4c4",black:"000",blanchedalmond:"ffebcd",blue:"0000ff",blueviolet:"8a2be2",brown:"a52a2a",burlywood:"deb887",cadetblue:"5f9ea0",chartreuse:"7fff00",chocolate:"d2691e",coral:"ff7f50",cornflowerblue:"6495ed",cornsilk:"fff8dc",crimson:"dc143c",cyan:"00ffff",darkblue:"00008b",darkcyan:"008b8b",darkgoldenrod:"b8860b",darkgray:"a9a9a9",darkgreen:"006400",darkgrey:"a9a9a9",darkkhaki:"bdb76b",darkmagenta:"8b008b",darkolivegreen:"556b2f",darkorange:"ff8c00",darkorchid:"9932cc",darkred:"8b0000",darksalmon:"e9967a",darkseagreen:"8fbc8f",darkslateblue:"483d8b",darkslategray:"2f4f4f",darkslategrey:"2f4f4f",darkturquoise:"00ced1",darkviolet:"9400d3",deeppink:"ff1493",deepskyblue:"00bfff",dimgray:"696969",dimgrey:"696969",dodgerblue:"1e90ff",firebrick:"b22222",floralwhite:"fffaf0",forestgreen:"228b22",fuchsia:"ff00ff",gainsboro:"dcdcdc",ghostwhite:"f8f8ff",gold:"ffd700",goldenrod:"daa520",gray:"808080",green:"008000",greenyellow:"adff2f",grey:"808080",honeydew:"f0fff0",hotpink:"ff69b4",indianred:"cd5c5c",indigo:"4b0082",ivory:"fffff0",khaki:"f0e68c",lavender:"e6e6fa",lavenderblush:"fff0f5",lawngreen:"7cfc00",lemonchiffon:"fffacd",lightblue:"add8e6",lightcoral:"f08080",lightcyan:"e0ffff",lightgoldenrodyellow:"fafad2",lightgray:"d3d3d3",lightgreen:"90ee90",lightgrey:"d3d3d3",lightpink:"ffb6c1",lightsalmon:"ffa07a",lightseagreen:"20b2aa",lightskyblue:"87cefa",lightslategray:"789",lightslategrey:"789",lightsteelblue:"b0c4de",lightyellow:"ffffe0",lime:"0f0",limegreen:"32cd32",linen:"faf0e6",magenta:"f0f",maroon:"800000",mediumaquamarine:"66cdaa",mediumblue:"0000cd",mediumorchid:"ba55d3",mediumpurple:"9370db",mediumseagreen:"3cb371",mediumslateblue:"7b68ee",mediumspringgreen:"00fa9a",mediumturquoise:"48d1cc",mediumvioletred:"c71585",midnightblue:"191970",mintcream:"f5fffa",mistyrose:"ffe4e1",moccasin:"ffe4b5",navajowhite:"ffdead",navy:"000080",oldlace:"fdf5e6",olive:"808000",olivedrab:"6b8e23",orange:"ffa500",orangered:"ff4500",orchid:"da70d6",palegoldenrod:"eee8aa",palegreen:"98fb98",paleturquoise:"afeeee",palevioletred:"db7093",papayawhip:"ffefd5",peachpuff:"ffdab9",peru:"cd853f",pink:"ffc0cb",plum:"dda0dd",powderblue:"b0e0e6",purple:"800080",rebeccapurple:"639",red:"f00",rosybrown:"bc8f8f",royalblue:"4169e1",saddlebrown:"8b4513",salmon:"fa8072",sandybrown:"f4a460",seagreen:"2e8b57",seashell:"fff5ee",sienna:"a0522d",silver:"c0c0c0",skyblue:"87ceeb",slateblue:"6a5acd",slategray:"708090",slategrey:"708090",snow:"fffafa",springgreen:"00ff7f",steelblue:"4682b4",tan:"d2b48c",teal:"008080",thistle:"d8bfd8",tomato:"ff6347",turquoise:"40e0d0",violet:"ee82ee",wheat:"f5deb3",white:"fff",whitesmoke:"f5f5f5",yellow:"ff0",yellowgreen:"9acd32"};function $P(i){if(typeof i!="string")return i;var e=i.toLowerCase();return xy[e]?"#"+xy[e]:i}var ZP=/^#[a-fA-F0-9]{6}$/,KP=/^#[a-fA-F0-9]{8}$/,JP=/^#[a-fA-F0-9]{3}$/,QP=/^#[a-fA-F0-9]{4}$/,vp=/^rgb\(\s*(\d{1,3})\s*(?:,)?\s*(\d{1,3})\s*(?:,)?\s*(\d{1,3})\s*\)$/i,e2=/^rgb(?:a)?\(\s*(\d{1,3})\s*(?:,)?\s*(\d{1,3})\s*(?:,)?\s*(\d{1,3})\s*(?:,|\/)\s*([-+]?\d*[.]?\d+[%]?)\s*\)$/i,t2=/^hsl\(\s*(\d{0,3}[.]?[0-9]+(?:deg)?)\s*(?:,)?\s*(\d{1,3}[.]?[0-9]?)%\s*(?:,)?\s*(\d{1,3}[.]?[0-9]?)%\s*\)$/i,n2=/^hsl(?:a)?\(\s*(\d{0,3}[.]?[0-9]+(?:deg)?)\s*(?:,)?\s*(\d{1,3}[.]?[0-9]?)%\s*(?:,)?\s*(\d{1,3}[.]?[0-9]?)%\s*(?:,|\/)\s*([-+]?\d*[.]?\d+[%]?)\s*\)$/i;function mr(i){if(typeof i!="string")throw new yn(3);var e=$P(i);if(e.match(ZP))return{red:parseInt(""+e[1]+e[2],16),green:parseInt(""+e[3]+e[4],16),blue:parseInt(""+e[5]+e[6],16)};if(e.match(KP)){var n=parseFloat((parseInt(""+e[7]+e[8],16)/255).toFixed(2));return{red:parseInt(""+e[1]+e[2],16),green:parseInt(""+e[3]+e[4],16),blue:parseInt(""+e[5]+e[6],16),alpha:n}}if(e.match(JP))return{red:parseInt(""+e[1]+e[1],16),green:parseInt(""+e[2]+e[2],16),blue:parseInt(""+e[3]+e[3],16)};if(e.match(QP)){var r=parseFloat((parseInt(""+e[4]+e[4],16)/255).toFixed(2));return{red:parseInt(""+e[1]+e[1],16),green:parseInt(""+e[2]+e[2],16),blue:parseInt(""+e[3]+e[3],16),alpha:r}}var s=vp.exec(e);if(s)return{red:parseInt(""+s[1],10),green:parseInt(""+s[2],10),blue:parseInt(""+s[3],10)};var o=e2.exec(e.substring(0,50));if(o)return{red:parseInt(""+o[1],10),green:parseInt(""+o[2],10),blue:parseInt(""+o[3],10),alpha:parseFloat(""+o[4])>1?parseFloat(""+o[4])/100:parseFloat(""+o[4])};var a=t2.exec(e);if(a){var l=parseInt(""+a[1],10),c=parseInt(""+a[2],10)/100,u=parseInt(""+a[3],10)/100,h="rgb("+Ta(l,c,u)+")",d=vp.exec(h);if(!d)throw new yn(4,e,h);return{red:parseInt(""+d[1],10),green:parseInt(""+d[2],10),blue:parseInt(""+d[3],10)}}var f=n2.exec(e.substring(0,50));if(f){var p=parseInt(""+f[1],10),g=parseInt(""+f[2],10)/100,v=parseInt(""+f[3],10)/100,_="rgb("+Ta(p,g,v)+")",m=vp.exec(_);if(!m)throw new yn(4,e,_);return{red:parseInt(""+m[1],10),green:parseInt(""+m[2],10),blue:parseInt(""+m[3],10),alpha:parseFloat(""+f[4])>1?parseFloat(""+f[4])/100:parseFloat(""+f[4])}}throw new yn(5)}function i2(i){var e=i.red/255,n=i.green/255,r=i.blue/255,s=Math.max(e,n,r),o=Math.min(e,n,r),a=(s+o)/2;if(s===o)return i.alpha!==void 0?{hue:0,saturation:0,lightness:a,alpha:i.alpha}:{hue:0,saturation:0,lightness:a};var l,c=s-o,u=a>.5?c/(2-s-o):c/(s+o);switch(s){case e:l=(n-r)/c+(n<r?6:0);break;case n:l=(r-e)/c+2;break;default:l=(e-n)/c+4;break}return l*=60,i.alpha!==void 0?{hue:l,saturation:u,lightness:a,alpha:i.alpha}:{hue:l,saturation:u,lightness:a}}function gr(i){return i2(mr(i))}var r2=function(e){return e.length===7&&e[1]===e[2]&&e[3]===e[4]&&e[5]===e[6]?"#"+e[1]+e[3]+e[5]:e},xp=r2;function $r(i){var e=i.toString(16);return e.length===1?"0"+e:e}function yp(i){return $r(Math.round(i*255))}function s2(i,e,n){return xp("#"+yp(i)+yp(e)+yp(n))}function nh(i,e,n){return Ta(i,e,n,s2)}function o2(i,e,n){if(typeof i=="number"&&typeof e=="number"&&typeof n=="number")return nh(i,e,n);if(typeof i=="object"&&e===void 0&&n===void 0)return nh(i.hue,i.saturation,i.lightness);throw new yn(1)}function a2(i,e,n,r){if(typeof i=="number"&&typeof e=="number"&&typeof n=="number"&&typeof r=="number")return r>=1?nh(i,e,n):"rgba("+Ta(i,e,n)+","+r+")";if(typeof i=="object"&&e===void 0&&n===void 0&&r===void 0)return i.alpha>=1?nh(i.hue,i.saturation,i.lightness):"rgba("+Ta(i.hue,i.saturation,i.lightness)+","+i.alpha+")";throw new yn(2)}function bp(i,e,n){if(typeof i=="number"&&typeof e=="number"&&typeof n=="number")return xp("#"+$r(i)+$r(e)+$r(n));if(typeof i=="object"&&e===void 0&&n===void 0)return xp("#"+$r(i.red)+$r(i.green)+$r(i.blue));throw new yn(6)}function ih(i,e,n,r){if(typeof i=="string"&&typeof e=="number"){var s=mr(i);return"rgba("+s.red+","+s.green+","+s.blue+","+e+")"}else{if(typeof i=="number"&&typeof e=="number"&&typeof n=="number"&&typeof r=="number")return r>=1?bp(i,e,n):"rgba("+i+","+e+","+n+","+r+")";if(typeof i=="object"&&e===void 0&&n===void 0&&r===void 0)return i.alpha>=1?bp(i.red,i.green,i.blue):"rgba("+i.red+","+i.green+","+i.blue+","+i.alpha+")"}throw new yn(7)}var l2=function(e){return typeof e.red=="number"&&typeof e.green=="number"&&typeof e.blue=="number"&&(typeof e.alpha!="number"||typeof e.alpha>"u")},c2=function(e){return typeof e.red=="number"&&typeof e.green=="number"&&typeof e.blue=="number"&&typeof e.alpha=="number"},u2=function(e){return typeof e.hue=="number"&&typeof e.saturation=="number"&&typeof e.lightness=="number"&&(typeof e.alpha!="number"||typeof e.alpha>"u")},h2=function(e){return typeof e.hue=="number"&&typeof e.saturation=="number"&&typeof e.lightness=="number"&&typeof e.alpha=="number"};function _r(i){if(typeof i!="object")throw new yn(8);if(c2(i))return ih(i);if(l2(i))return bp(i);if(h2(i))return a2(i);if(u2(i))return o2(i);throw new yn(8)}function Sy(i,e,n){return function(){var s=n.concat(Array.prototype.slice.call(arguments));return s.length>=e?i.apply(this,s):Sy(i,e,s)}}function Tn(i){return Sy(i,i.length,[])}function f2(i,e){if(e==="transparent")return e;var n=gr(e);return _r(un({},n,{hue:n.hue+parseFloat(i)}))}var f4=Tn(f2);function ro(i,e,n){return Math.max(i,Math.min(e,n))}function d2(i,e){if(e==="transparent")return e;var n=gr(e);return _r(un({},n,{lightness:ro(0,1,n.lightness-parseFloat(i))}))}var d4=Tn(d2);function p2(i,e){if(e==="transparent")return e;var n=gr(e);return _r(un({},n,{saturation:ro(0,1,n.saturation-parseFloat(i))}))}var p4=Tn(p2);function m2(i,e){if(e==="transparent")return e;var n=gr(e);return _r(un({},n,{lightness:ro(0,1,n.lightness+parseFloat(i))}))}var m4=Tn(m2);function g2(i,e,n){if(e==="transparent")return n;if(n==="transparent")return e;if(i===0)return n;var r=mr(e),s=un({},r,{alpha:typeof r.alpha=="number"?r.alpha:1}),o=mr(n),a=un({},o,{alpha:typeof o.alpha=="number"?o.alpha:1}),l=s.alpha-a.alpha,c=parseFloat(i)*2-1,u=c*l===-1?c:c+l,h=1+c*l,d=(u/h+1)/2,f=1-d,p={red:Math.floor(s.red*d+a.red*f),green:Math.floor(s.green*d+a.green*f),blue:Math.floor(s.blue*d+a.blue*f),alpha:s.alpha*parseFloat(i)+a.alpha*(1-parseFloat(i))};return ih(p)}var _2=Tn(g2),wy=_2;function v2(i,e){if(e==="transparent")return e;var n=mr(e),r=typeof n.alpha=="number"?n.alpha:1,s=un({},n,{alpha:ro(0,1,(r*100+parseFloat(i)*100)/100)});return ih(s)}var y2=Tn(v2),My=y2;function x2(i,e){if(e==="transparent")return e;var n=gr(e);return _r(un({},n,{saturation:ro(0,1,n.saturation+parseFloat(i))}))}var g4=Tn(x2);function b2(i,e){return e==="transparent"?e:_r(un({},gr(e),{hue:parseFloat(i)}))}var _4=Tn(b2);function S2(i,e){return e==="transparent"?e:_r(un({},gr(e),{lightness:parseFloat(i)}))}var v4=Tn(S2);function w2(i,e){return e==="transparent"?e:_r(un({},gr(e),{saturation:parseFloat(i)}))}var y4=Tn(w2);function M2(i,e){return e==="transparent"?e:wy(parseFloat(i),"rgb(0, 0, 0)",e)}var x4=Tn(M2);function E2(i,e){return e==="transparent"?e:wy(parseFloat(i),"rgb(255, 255, 255)",e)}var b4=Tn(E2);function A2(i,e){if(e==="transparent")return e;var n=mr(e),r=typeof n.alpha=="number"?n.alpha:1,s=un({},n,{alpha:ro(0,1,+(r*100-parseFloat(i)*100).toFixed(2)/100)});return ih(s)}var S4=Tn(A2);var si=Object.freeze({Linear:Object.freeze({None:function(i){return i},In:function(i){return i},Out:function(i){return i},InOut:function(i){return i}}),Quadratic:Object.freeze({In:function(i){return i*i},Out:function(i){return i*(2-i)},InOut:function(i){return(i*=2)<1?.5*i*i:-.5*(--i*(i-2)-1)}}),Cubic:Object.freeze({In:function(i){return i*i*i},Out:function(i){return--i*i*i+1},InOut:function(i){return(i*=2)<1?.5*i*i*i:.5*((i-=2)*i*i+2)}}),Quartic:Object.freeze({In:function(i){return i*i*i*i},Out:function(i){return 1- --i*i*i*i},InOut:function(i){return(i*=2)<1?.5*i*i*i*i:-.5*((i-=2)*i*i*i-2)}}),Quintic:Object.freeze({In:function(i){return i*i*i*i*i},Out:function(i){return--i*i*i*i*i+1},InOut:function(i){return(i*=2)<1?.5*i*i*i*i*i:.5*((i-=2)*i*i*i*i+2)}}),Sinusoidal:Object.freeze({In:function(i){return 1-Math.sin((1-i)*Math.PI/2)},Out:function(i){return Math.sin(i*Math.PI/2)},InOut:function(i){return .5*(1-Math.sin(Math.PI*(.5-i)))}}),Exponential:Object.freeze({In:function(i){return i===0?0:Math.pow(1024,i-1)},Out:function(i){return i===1?1:1-Math.pow(2,-10*i)},InOut:function(i){return i===0?0:i===1?1:(i*=2)<1?.5*Math.pow(1024,i-1):.5*(-Math.pow(2,-10*(i-1))+2)}}),Circular:Object.freeze({In:function(i){return 1-Math.sqrt(1-i*i)},Out:function(i){return Math.sqrt(1- --i*i)},InOut:function(i){return(i*=2)<1?-.5*(Math.sqrt(1-i*i)-1):.5*(Math.sqrt(1-(i-=2)*i)+1)}}),Elastic:Object.freeze({In:function(i){return i===0?0:i===1?1:-Math.pow(2,10*(i-1))*Math.sin((i-1.1)*5*Math.PI)},Out:function(i){return i===0?0:i===1?1:Math.pow(2,-10*i)*Math.sin((i-.1)*5*Math.PI)+1},InOut:function(i){return i===0?0:i===1?1:(i*=2,i<1?-.5*Math.pow(2,10*(i-1))*Math.sin((i-1.1)*5*Math.PI):.5*Math.pow(2,-10*(i-1))*Math.sin((i-1.1)*5*Math.PI)+1)}}),Back:Object.freeze({In:function(i){var e=1.70158;return i===1?1:i*i*((e+1)*i-e)},Out:function(i){var e=1.70158;return i===0?0:--i*i*((e+1)*i+e)+1},InOut:function(i){var e=2.5949095;return(i*=2)<1?.5*(i*i*((e+1)*i-e)):.5*((i-=2)*i*((e+1)*i+e)+2)}}),Bounce:Object.freeze({In:function(i){return 1-si.Bounce.Out(1-i)},Out:function(i){return i<.36363636363636365?7.5625*i*i:i<.7272727272727273?7.5625*(i-=.5454545454545454)*i+.75:i<.9090909090909091?7.5625*(i-=.8181818181818182)*i+.9375:7.5625*(i-=.9545454545454546)*i+.984375},InOut:function(i){return i<.5?si.Bounce.In(i*2)*.5:si.Bounce.Out(i*2-1)*.5+.5}}),generatePow:function(i){return i===void 0&&(i=4),i=i<Number.EPSILON?Number.EPSILON:i,i=i>1e4?1e4:i,{In:function(e){return Math.pow(e,i)},Out:function(e){return 1-Math.pow(1-e,i)},InOut:function(e){return e<.5?Math.pow(e*2,i)/2:(1-Math.pow(2-e*2,i))/2+.5}}}}),Ca=function(){return performance.now()},Ra=(function(){function i(){for(var e=[],n=0;n<arguments.length;n++)e[n]=arguments[n];this._tweens={},this._tweensAddedDuringUpdate={},this.add.apply(this,e)}return i.prototype.getAll=function(){var e=this;return Object.keys(this._tweens).map(function(n){return e._tweens[n]})},i.prototype.removeAll=function(){this._tweens={}},i.prototype.add=function(){for(var e,n=[],r=0;r<arguments.length;r++)n[r]=arguments[r];for(var s=0,o=n;s<o.length;s++){var a=o[s];(e=a._group)===null||e===void 0||e.remove(a),a._group=this,this._tweens[a.getId()]=a,this._tweensAddedDuringUpdate[a.getId()]=a}},i.prototype.remove=function(){for(var e=[],n=0;n<arguments.length;n++)e[n]=arguments[n];for(var r=0,s=e;r<s.length;r++){var o=s[r];o._group=void 0,delete this._tweens[o.getId()],delete this._tweensAddedDuringUpdate[o.getId()]}},i.prototype.allStopped=function(){return this.getAll().every(function(e){return!e.isPlaying()})},i.prototype.update=function(e,n){e===void 0&&(e=Ca()),n===void 0&&(n=!0);var r=Object.keys(this._tweens);if(r.length!==0)for(;r.length>0;){this._tweensAddedDuringUpdate={};for(var s=0;s<r.length;s++){var o=this._tweens[r[s]],a=!n;o&&o.update(e,a)===!1&&!n&&this.remove(o)}r=Object.keys(this._tweensAddedDuringUpdate)}},i})(),so={Linear:function(i,e){var n=i.length-1,r=n*e,s=Math.floor(r),o=so.Utils.Linear;return e<0?o(i[0],i[1],r):e>1?o(i[n],i[n-1],n-r):o(i[s],i[s+1>n?n:s+1],r-s)},Bezier:function(i,e){for(var n=0,r=i.length-1,s=Math.pow,o=so.Utils.Bernstein,a=0;a<=r;a++)n+=s(1-e,r-a)*s(e,a)*i[a]*o(r,a);return n},CatmullRom:function(i,e){var n=i.length-1,r=n*e,s=Math.floor(r),o=so.Utils.CatmullRom;return i[0]===i[n]?(e<0&&(s=Math.floor(r=n*(1+e))),o(i[(s-1+n)%n],i[s],i[(s+1)%n],i[(s+2)%n],r-s)):e<0?i[0]-(o(i[0],i[0],i[1],i[1],-r)-i[0]):e>1?i[n]-(o(i[n],i[n],i[n-1],i[n-1],r-n)-i[n]):o(i[s?s-1:0],i[s],i[n<s+1?n:s+1],i[n<s+2?n:s+2],r-s)},Utils:{Linear:function(i,e,n){return(e-i)*n+i},Bernstein:function(i,e){var n=so.Utils.Factorial;return n(i)/n(e)/n(i-e)},Factorial:(function(){var i=[1];return function(e){var n=1;if(i[e])return i[e];for(var r=e;r>1;r--)n*=r;return i[e]=n,n}})(),CatmullRom:function(i,e,n,r,s){var o=(n-i)*.5,a=(r-e)*.5,l=s*s,c=s*l;return(2*e-2*n+o+a)*c+(-3*e+3*n-2*o-a)*l+o*s+e}}},Ey=(function(){function i(){}return i.nextId=function(){return i._nextId++},i._nextId=0,i})(),Sp=new Ra,oo=(function(){function i(e,n){this._isPaused=!1,this._pauseStart=0,this._valuesStart={},this._valuesEnd={},this._valuesStartRepeat={},this._duration=1e3,this._isDynamic=!1,this._initialRepeat=0,this._repeat=0,this._yoyo=!1,this._isPlaying=!1,this._reversed=!1,this._delayTime=0,this._startTime=0,this._easingFunction=si.Linear.None,this._interpolationFunction=so.Linear,this._chainedTweens=[],this._onStartCallbackFired=!1,this._onEveryStartCallbackFired=!1,this._id=Ey.nextId(),this._isChainStopped=!1,this._propertiesAreSetUp=!1,this._goToEnd=!1,this._object=e,typeof n=="object"?(this._group=n,n.add(this)):n===!0&&(this._group=Sp,Sp.add(this))}return i.prototype.getId=function(){return this._id},i.prototype.isPlaying=function(){return this._isPlaying},i.prototype.isPaused=function(){return this._isPaused},i.prototype.getDuration=function(){return this._duration},i.prototype.to=function(e,n){if(n===void 0&&(n=1e3),this._isPlaying)throw new Error("Can not call Tween.to() while Tween is already started or paused. Stop the Tween first.");return this._valuesEnd=e,this._propertiesAreSetUp=!1,this._duration=n<0?0:n,this},i.prototype.duration=function(e){return e===void 0&&(e=1e3),this._duration=e<0?0:e,this},i.prototype.dynamic=function(e){return e===void 0&&(e=!1),this._isDynamic=e,this},i.prototype.start=function(e,n){if(e===void 0&&(e=Ca()),n===void 0&&(n=!1),this._isPlaying)return this;if(this._repeat=this._initialRepeat,this._reversed){this._reversed=!1;for(var r in this._valuesStartRepeat)this._swapEndStartRepeatValues(r),this._valuesStart[r]=this._valuesStartRepeat[r]}if(this._isPlaying=!0,this._isPaused=!1,this._onStartCallbackFired=!1,this._onEveryStartCallbackFired=!1,this._isChainStopped=!1,this._startTime=e,this._startTime+=this._delayTime,!this._propertiesAreSetUp||n){if(this._propertiesAreSetUp=!0,!this._isDynamic){var s={};for(var o in this._valuesEnd)s[o]=this._valuesEnd[o];this._valuesEnd=s}this._setupProperties(this._object,this._valuesStart,this._valuesEnd,this._valuesStartRepeat,n)}return this},i.prototype.startFromCurrentValues=function(e){return this.start(e,!0)},i.prototype._setupProperties=function(e,n,r,s,o){for(var a in r){var l=e[a],c=Array.isArray(l),u=c?"array":typeof l,h=!c&&Array.isArray(r[a]);if(!(u==="undefined"||u==="function")){if(h){var d=r[a];if(d.length===0)continue;for(var f=[l],p=0,g=d.length;p<g;p+=1){var v=this._handleRelativeValue(l,d[p]);if(isNaN(v)){h=!1,console.warn("Found invalid interpolation list. Skipping.");break}f.push(v)}h&&(r[a]=f)}if((u==="object"||c)&&l&&!h){n[a]=c?[]:{};var _=l;for(var m in _)n[a][m]=_[m];s[a]=c?[]:{};var d=r[a];if(!this._isDynamic){var w={};for(var m in d)w[m]=d[m];r[a]=d=w}this._setupProperties(_,n[a],d,s[a],o)}else(typeof n[a]>"u"||o)&&(n[a]=l),c||(n[a]*=1),h?s[a]=r[a].slice().reverse():s[a]=n[a]||0}}},i.prototype.stop=function(){return this._isChainStopped||(this._isChainStopped=!0,this.stopChainedTweens()),this._isPlaying?(this._isPlaying=!1,this._isPaused=!1,this._onStopCallback&&this._onStopCallback(this._object),this):this},i.prototype.end=function(){return this._goToEnd=!0,this.update(this._startTime+this._duration),this},i.prototype.pause=function(e){return e===void 0&&(e=Ca()),this._isPaused||!this._isPlaying?this:(this._isPaused=!0,this._pauseStart=e,this)},i.prototype.resume=function(e){return e===void 0&&(e=Ca()),!this._isPaused||!this._isPlaying?this:(this._isPaused=!1,this._startTime+=e-this._pauseStart,this._pauseStart=0,this)},i.prototype.stopChainedTweens=function(){for(var e=0,n=this._chainedTweens.length;e<n;e++)this._chainedTweens[e].stop();return this},i.prototype.group=function(e){return e?(e.add(this),this):(console.warn("tween.group() without args has been removed, use group.add(tween) instead."),this)},i.prototype.remove=function(){var e;return(e=this._group)===null||e===void 0||e.remove(this),this},i.prototype.delay=function(e){return e===void 0&&(e=0),this._delayTime=e,this},i.prototype.repeat=function(e){return e===void 0&&(e=0),this._initialRepeat=e,this._repeat=e,this},i.prototype.repeatDelay=function(e){return this._repeatDelayTime=e,this},i.prototype.yoyo=function(e){return e===void 0&&(e=!1),this._yoyo=e,this},i.prototype.easing=function(e){return e===void 0&&(e=si.Linear.None),this._easingFunction=e,this},i.prototype.interpolation=function(e){return e===void 0&&(e=so.Linear),this._interpolationFunction=e,this},i.prototype.chain=function(){for(var e=[],n=0;n<arguments.length;n++)e[n]=arguments[n];return this._chainedTweens=e,this},i.prototype.onStart=function(e){return this._onStartCallback=e,this},i.prototype.onEveryStart=function(e){return this._onEveryStartCallback=e,this},i.prototype.onUpdate=function(e){return this._onUpdateCallback=e,this},i.prototype.onRepeat=function(e){return this._onRepeatCallback=e,this},i.prototype.onComplete=function(e){return this._onCompleteCallback=e,this},i.prototype.onStop=function(e){return this._onStopCallback=e,this},i.prototype.update=function(e,n){var r=this,s;if(e===void 0&&(e=Ca()),n===void 0&&(n=i.autoStartOnUpdate),this._isPaused)return!0;var o;if(!this._goToEnd&&!this._isPlaying)if(n)this.start(e,!0);else return!1;if(this._goToEnd=!1,e<this._startTime)return!0;this._onStartCallbackFired===!1&&(this._onStartCallback&&this._onStartCallback(this._object),this._onStartCallbackFired=!0),this._onEveryStartCallbackFired===!1&&(this._onEveryStartCallback&&this._onEveryStartCallback(this._object),this._onEveryStartCallbackFired=!0);var a=e-this._startTime,l=this._duration+((s=this._repeatDelayTime)!==null&&s!==void 0?s:this._delayTime),c=this._duration+this._repeat*l,u=function(){if(r._duration===0||a>c)return 1;var v=Math.trunc(a/l),_=a-v*l,m=Math.min(_/r._duration,1);return m===0&&a===r._duration?1:m},h=u(),d=this._easingFunction(h);if(this._updateProperties(this._object,this._valuesStart,this._valuesEnd,d),this._onUpdateCallback&&this._onUpdateCallback(this._object,h),this._duration===0||a>=this._duration)if(this._repeat>0){var f=Math.min(Math.trunc((a-this._duration)/l)+1,this._repeat);isFinite(this._repeat)&&(this._repeat-=f);for(o in this._valuesStartRepeat)!this._yoyo&&typeof this._valuesEnd[o]=="string"&&(this._valuesStartRepeat[o]=this._valuesStartRepeat[o]+parseFloat(this._valuesEnd[o])),this._yoyo&&this._swapEndStartRepeatValues(o),this._valuesStart[o]=this._valuesStartRepeat[o];return this._yoyo&&(this._reversed=!this._reversed),this._startTime+=l*f,this._onRepeatCallback&&this._onRepeatCallback(this._object),this._onEveryStartCallbackFired=!1,!0}else{this._onCompleteCallback&&this._onCompleteCallback(this._object);for(var p=0,g=this._chainedTweens.length;p<g;p++)this._chainedTweens[p].start(this._startTime+this._duration,!1);return this._isPlaying=!1,!1}return!0},i.prototype._updateProperties=function(e,n,r,s){for(var o in r)if(n[o]!==void 0){var a=n[o]||0,l=r[o],c=Array.isArray(e[o]),u=Array.isArray(l),h=!c&&u;h?e[o]=this._interpolationFunction(l,s):typeof l=="object"&&l?this._updateProperties(e[o],a,l,s):(l=this._handleRelativeValue(a,l),typeof l=="number"&&(e[o]=a+(l-a)*s))}},i.prototype._handleRelativeValue=function(e,n){return typeof n!="string"?n:n.charAt(0)==="+"||n.charAt(0)==="-"?e+parseFloat(n):parseFloat(n)},i.prototype._swapEndStartRepeatValues=function(e){var n=this._valuesStartRepeat[e],r=this._valuesEnd[e];typeof r=="string"?this._valuesStartRepeat[e]=this._valuesStartRepeat[e]+parseFloat(r):this._valuesStartRepeat[e]=this._valuesEnd[e],this._valuesEnd[e]=n},i.autoStartOnUpdate=!1,i})();var M4=Ey.nextId,vi=Sp,E4=vi.getAll.bind(vi),A4=vi.removeAll.bind(vi),T4=vi.add.bind(vi),C4=vi.remove.bind(vi),R4=vi.update.bind(vi);var rh="http://www.w3.org/1999/xhtml",wp={svg:"http://www.w3.org/2000/svg",xhtml:rh,xlink:"http://www.w3.org/1999/xlink",xml:"http://www.w3.org/XML/1998/namespace",xmlns:"http://www.w3.org/2000/xmlns/"};function Ui(i){var e=i+="",n=e.indexOf(":");return n>=0&&(e=i.slice(0,n))!=="xmlns"&&(i=i.slice(n+1)),wp.hasOwnProperty(e)?{space:wp[e],local:i}:i}function T2(i){return function(){var e=this.ownerDocument,n=this.namespaceURI;return n===rh&&e.documentElement.namespaceURI===rh?e.createElement(i):e.createElementNS(n,i)}}function C2(i){return function(){return this.ownerDocument.createElementNS(i.space,i.local)}}function sh(i){var e=Ui(i);return(e.local?C2:T2)(e)}function R2(){}function Zr(i){return i==null?R2:function(){return this.querySelector(i)}}function Ay(i){typeof i!="function"&&(i=Zr(i));for(var e=this._groups,n=e.length,r=new Array(n),s=0;s<n;++s)for(var o=e[s],a=o.length,l=r[s]=new Array(a),c,u,h=0;h<a;++h)(c=o[h])&&(u=i.call(c,c.__data__,h,o))&&("__data__"in c&&(u.__data__=c.__data__),l[h]=u);return new wt(r,this._parents)}function Mp(i){return i==null?[]:Array.isArray(i)?i:Array.from(i)}function P2(){return[]}function Pa(i){return i==null?P2:function(){return this.querySelectorAll(i)}}function I2(i){return function(){return Mp(i.apply(this,arguments))}}function Ty(i){typeof i=="function"?i=I2(i):i=Pa(i);for(var e=this._groups,n=e.length,r=[],s=[],o=0;o<n;++o)for(var a=e[o],l=a.length,c,u=0;u<l;++u)(c=a[u])&&(r.push(i.call(c,c.__data__,u,a)),s.push(c));return new wt(r,s)}function Ia(i){return function(){return this.matches(i)}}function oh(i){return function(e){return e.matches(i)}}var L2=Array.prototype.find;function D2(i){return function(){return L2.call(this.children,i)}}function O2(){return this.firstElementChild}function Cy(i){return this.select(i==null?O2:D2(typeof i=="function"?i:oh(i)))}var N2=Array.prototype.filter;function U2(){return Array.from(this.children)}function F2(i){return function(){return N2.call(this.children,i)}}function Ry(i){return this.selectAll(i==null?U2:F2(typeof i=="function"?i:oh(i)))}function Py(i){typeof i!="function"&&(i=Ia(i));for(var e=this._groups,n=e.length,r=new Array(n),s=0;s<n;++s)for(var o=e[s],a=o.length,l=r[s]=[],c,u=0;u<a;++u)(c=o[u])&&i.call(c,c.__data__,u,o)&&l.push(c);return new wt(r,this._parents)}function ah(i){return new Array(i.length)}function Iy(){return new wt(this._enter||this._groups.map(ah),this._parents)}function La(i,e){this.ownerDocument=i.ownerDocument,this.namespaceURI=i.namespaceURI,this._next=null,this._parent=i,this.__data__=e}La.prototype={constructor:La,appendChild:function(i){return this._parent.insertBefore(i,this._next)},insertBefore:function(i,e){return this._parent.insertBefore(i,e)},querySelector:function(i){return this._parent.querySelector(i)},querySelectorAll:function(i){return this._parent.querySelectorAll(i)}};function Ly(i){return function(){return i}}function k2(i,e,n,r,s,o){for(var a=0,l,c=e.length,u=o.length;a<u;++a)(l=e[a])?(l.__data__=o[a],r[a]=l):n[a]=new La(i,o[a]);for(;a<c;++a)(l=e[a])&&(s[a]=l)}function B2(i,e,n,r,s,o,a){var l,c,u=new Map,h=e.length,d=o.length,f=new Array(h),p;for(l=0;l<h;++l)(c=e[l])&&(f[l]=p=a.call(c,c.__data__,l,e)+"",u.has(p)?s[l]=c:u.set(p,c));for(l=0;l<d;++l)p=a.call(i,o[l],l,o)+"",(c=u.get(p))?(r[l]=c,c.__data__=o[l],u.delete(p)):n[l]=new La(i,o[l]);for(l=0;l<h;++l)(c=e[l])&&u.get(f[l])===c&&(s[l]=c)}function z2(i){return i.__data__}function Dy(i,e){if(!arguments.length)return Array.from(this,z2);var n=e?B2:k2,r=this._parents,s=this._groups;typeof i!="function"&&(i=Ly(i));for(var o=s.length,a=new Array(o),l=new Array(o),c=new Array(o),u=0;u<o;++u){var h=r[u],d=s[u],f=d.length,p=V2(i.call(h,h&&h.__data__,u,r)),g=p.length,v=l[u]=new Array(g),_=a[u]=new Array(g),m=c[u]=new Array(f);n(h,d,v,_,m,p,e);for(var w=0,T=0,y,b;w<g;++w)if(y=v[w]){for(w>=T&&(T=w+1);!(b=_[T])&&++T<g;);y._next=b||null}}return a=new wt(a,r),a._enter=l,a._exit=c,a}function V2(i){return typeof i=="object"&&"length"in i?i:Array.from(i)}function Oy(){return new wt(this._exit||this._groups.map(ah),this._parents)}function Ny(i,e,n){var r=this.enter(),s=this,o=this.exit();return typeof i=="function"?(r=i(r),r&&(r=r.selection())):r=r.append(i+""),e!=null&&(s=e(s),s&&(s=s.selection())),n==null?o.remove():n(o),r&&s?r.merge(s).order():s}function Uy(i){for(var e=i.selection?i.selection():i,n=this._groups,r=e._groups,s=n.length,o=r.length,a=Math.min(s,o),l=new Array(s),c=0;c<a;++c)for(var u=n[c],h=r[c],d=u.length,f=l[c]=new Array(d),p,g=0;g<d;++g)(p=u[g]||h[g])&&(f[g]=p);for(;c<s;++c)l[c]=n[c];return new wt(l,this._parents)}function Fy(){for(var i=this._groups,e=-1,n=i.length;++e<n;)for(var r=i[e],s=r.length-1,o=r[s],a;--s>=0;)(a=r[s])&&(o&&a.compareDocumentPosition(o)^4&&o.parentNode.insertBefore(a,o),o=a);return this}function ky(i){i||(i=G2);function e(d,f){return d&&f?i(d.__data__,f.__data__):!d-!f}for(var n=this._groups,r=n.length,s=new Array(r),o=0;o<r;++o){for(var a=n[o],l=a.length,c=s[o]=new Array(l),u,h=0;h<l;++h)(u=a[h])&&(c[h]=u);c.sort(e)}return new wt(s,this._parents).order()}function G2(i,e){return i<e?-1:i>e?1:i>=e?0:NaN}function By(){var i=arguments[0];return arguments[0]=this,i.apply(null,arguments),this}function zy(){return Array.from(this)}function Vy(){for(var i=this._groups,e=0,n=i.length;e<n;++e)for(var r=i[e],s=0,o=r.length;s<o;++s){var a=r[s];if(a)return a}return null}function Gy(){let i=0;for(let e of this)++i;return i}function Hy(){return!this.node()}function Wy(i){for(var e=this._groups,n=0,r=e.length;n<r;++n)for(var s=e[n],o=0,a=s.length,l;o<a;++o)(l=s[o])&&i.call(l,l.__data__,o,s);return this}function H2(i){return function(){this.removeAttribute(i)}}function W2(i){return function(){this.removeAttributeNS(i.space,i.local)}}function X2(i,e){return function(){this.setAttribute(i,e)}}function q2(i,e){return function(){this.setAttributeNS(i.space,i.local,e)}}function j2(i,e){return function(){var n=e.apply(this,arguments);n==null?this.removeAttribute(i):this.setAttribute(i,n)}}function Y2(i,e){return function(){var n=e.apply(this,arguments);n==null?this.removeAttributeNS(i.space,i.local):this.setAttributeNS(i.space,i.local,n)}}function Xy(i,e){var n=Ui(i);if(arguments.length<2){var r=this.node();return n.local?r.getAttributeNS(n.space,n.local):r.getAttribute(n)}return this.each((e==null?n.local?W2:H2:typeof e=="function"?n.local?Y2:j2:n.local?q2:X2)(n,e))}function lh(i){return i.ownerDocument&&i.ownerDocument.defaultView||i.document&&i||i.defaultView}function $2(i){return function(){this.style.removeProperty(i)}}function Z2(i,e,n){return function(){this.style.setProperty(i,e,n)}}function K2(i,e,n){return function(){var r=e.apply(this,arguments);r==null?this.style.removeProperty(i):this.style.setProperty(i,r,n)}}function qy(i,e,n){return arguments.length>1?this.each((e==null?$2:typeof e=="function"?K2:Z2)(i,e,n??"")):vr(this.node(),i)}function vr(i,e){return i.style.getPropertyValue(e)||lh(i).getComputedStyle(i,null).getPropertyValue(e)}function J2(i){return function(){delete this[i]}}function Q2(i,e){return function(){this[i]=e}}function eI(i,e){return function(){var n=e.apply(this,arguments);n==null?delete this[i]:this[i]=n}}function jy(i,e){return arguments.length>1?this.each((e==null?J2:typeof e=="function"?eI:Q2)(i,e)):this.node()[i]}function Yy(i){return i.trim().split(/^|\s+/)}function Ep(i){return i.classList||new $y(i)}function $y(i){this._node=i,this._names=Yy(i.getAttribute("class")||"")}$y.prototype={add:function(i){var e=this._names.indexOf(i);e<0&&(this._names.push(i),this._node.setAttribute("class",this._names.join(" ")))},remove:function(i){var e=this._names.indexOf(i);e>=0&&(this._names.splice(e,1),this._node.setAttribute("class",this._names.join(" ")))},contains:function(i){return this._names.indexOf(i)>=0}};function Zy(i,e){for(var n=Ep(i),r=-1,s=e.length;++r<s;)n.add(e[r])}function Ky(i,e){for(var n=Ep(i),r=-1,s=e.length;++r<s;)n.remove(e[r])}function tI(i){return function(){Zy(this,i)}}function nI(i){return function(){Ky(this,i)}}function iI(i,e){return function(){(e.apply(this,arguments)?Zy:Ky)(this,i)}}function Jy(i,e){var n=Yy(i+"");if(arguments.length<2){for(var r=Ep(this.node()),s=-1,o=n.length;++s<o;)if(!r.contains(n[s]))return!1;return!0}return this.each((typeof e=="function"?iI:e?tI:nI)(n,e))}function rI(){this.textContent=""}function sI(i){return function(){this.textContent=i}}function oI(i){return function(){var e=i.apply(this,arguments);this.textContent=e??""}}function Qy(i){return arguments.length?this.each(i==null?rI:(typeof i=="function"?oI:sI)(i)):this.node().textContent}function aI(){this.innerHTML=""}function lI(i){return function(){this.innerHTML=i}}function cI(i){return function(){var e=i.apply(this,arguments);this.innerHTML=e??""}}function ex(i){return arguments.length?this.each(i==null?aI:(typeof i=="function"?cI:lI)(i)):this.node().innerHTML}function uI(){this.nextSibling&&this.parentNode.appendChild(this)}function tx(){return this.each(uI)}function hI(){this.previousSibling&&this.parentNode.insertBefore(this,this.parentNode.firstChild)}function nx(){return this.each(hI)}function ix(i){var e=typeof i=="function"?i:sh(i);return this.select(function(){return this.appendChild(e.apply(this,arguments))})}function fI(){return null}function rx(i,e){var n=typeof i=="function"?i:sh(i),r=e==null?fI:typeof e=="function"?e:Zr(e);return this.select(function(){return this.insertBefore(n.apply(this,arguments),r.apply(this,arguments)||null)})}function dI(){var i=this.parentNode;i&&i.removeChild(this)}function sx(){return this.each(dI)}function pI(){var i=this.cloneNode(!1),e=this.parentNode;return e?e.insertBefore(i,this.nextSibling):i}function mI(){var i=this.cloneNode(!0),e=this.parentNode;return e?e.insertBefore(i,this.nextSibling):i}function ox(i){return this.select(i?mI:pI)}function ax(i){return arguments.length?this.property("__data__",i):this.node().__data__}function gI(i){return function(e){i.call(this,e,this.__data__)}}function _I(i){return i.trim().split(/^|\s+/).map(function(e){var n="",r=e.indexOf(".");return r>=0&&(n=e.slice(r+1),e=e.slice(0,r)),{type:e,name:n}})}function vI(i){return function(){var e=this.__on;if(e){for(var n=0,r=-1,s=e.length,o;n<s;++n)o=e[n],(!i.type||o.type===i.type)&&o.name===i.name?this.removeEventListener(o.type,o.listener,o.options):e[++r]=o;++r?e.length=r:delete this.__on}}}function yI(i,e,n){return function(){var r=this.__on,s,o=gI(e);if(r){for(var a=0,l=r.length;a<l;++a)if((s=r[a]).type===i.type&&s.name===i.name){this.removeEventListener(s.type,s.listener,s.options),this.addEventListener(s.type,s.listener=o,s.options=n),s.value=e;return}}this.addEventListener(i.type,o,n),s={type:i.type,name:i.name,value:e,listener:o,options:n},r?r.push(s):this.__on=[s]}}function lx(i,e,n){var r=_I(i+""),s,o=r.length,a;if(arguments.length<2){var l=this.node().__on;if(l){for(var c=0,u=l.length,h;c<u;++c)for(s=0,h=l[c];s<o;++s)if((a=r[s]).type===h.type&&a.name===h.name)return h.value}return}for(l=e?yI:vI,s=0;s<o;++s)this.each(l(r[s],e,n));return this}function cx(i,e,n){var r=lh(i),s=r.CustomEvent;typeof s=="function"?s=new s(e,n):(s=r.document.createEvent("Event"),n?(s.initEvent(e,n.bubbles,n.cancelable),s.detail=n.detail):s.initEvent(e,!1,!1)),i.dispatchEvent(s)}function xI(i,e){return function(){return cx(this,i,e)}}function bI(i,e){return function(){return cx(this,i,e.apply(this,arguments))}}function ux(i,e){return this.each((typeof e=="function"?bI:xI)(i,e))}function*hx(){for(var i=this._groups,e=0,n=i.length;e<n;++e)for(var r=i[e],s=0,o=r.length,a;s<o;++s)(a=r[s])&&(yield a)}var Ap=[null];function wt(i,e){this._groups=i,this._parents=e}function fx(){return new wt([[document.documentElement]],Ap)}function SI(){return this}wt.prototype=fx.prototype={constructor:wt,select:Ay,selectAll:Ty,selectChild:Cy,selectChildren:Ry,filter:Py,data:Dy,enter:Iy,exit:Oy,join:Ny,merge:Uy,selection:SI,order:Fy,sort:ky,call:By,nodes:zy,node:Vy,size:Gy,empty:Hy,each:Wy,attr:Xy,style:qy,property:jy,classed:Jy,text:Qy,html:ex,raise:tx,lower:nx,append:ix,insert:rx,remove:sx,clone:ox,datum:ax,on:lx,dispatch:ux,[Symbol.iterator]:hx};var Fi=fx;function zt(i){return typeof i=="string"?new wt([[document.querySelector(i)]],[document.documentElement]):new wt([[i]],Ap)}function dx(i){let e;for(;e=i.sourceEvent;)i=e;return i}function xn(i,e){if(i=dx(i),e===void 0&&(e=i.currentTarget),e){var n=e.ownerSVGElement||e;if(n.createSVGPoint){var r=n.createSVGPoint();return r.x=i.clientX,r.y=i.clientY,r=r.matrixTransform(e.getScreenCTM().inverse()),[r.x,r.y]}if(e.getBoundingClientRect){var s=e.getBoundingClientRect();return[i.clientX-s.left-e.clientLeft,i.clientY-s.top-e.clientTop]}}return[i.pageX,i.pageY]}var Na,xt,vx,Pp,yr,px,yx,xx,Tp,uh,Da,bx,Ip,Cp,Rp,wI,fh={},dh=[],MI=/acit|ex(?:s|g|n|p|$)|rph|grid|ows|mnc|ntw|ine[ch]|zoo|^ord|itera/i,mh=Array.isArray;function yi(i,e){for(var n in e)i[n]=e[n];return i}function Lp(i){i&&i.parentNode&&i.parentNode.removeChild(i)}function EI(i,e,n){var r,s,o,a={};for(o in e)o=="key"?r=e[o]:o=="ref"?s=e[o]:a[o]=e[o];if(arguments.length>2&&(a.children=arguments.length>3?Na.call(arguments,2):n),typeof i=="function"&&i.defaultProps!=null)for(o in i.defaultProps)a[o]===void 0&&(a[o]=i.defaultProps[o]);return Oa(i,a,r,s,null)}function Oa(i,e,n,r,s){var o={type:i,props:e,key:n,ref:r,__k:null,__:null,__b:0,__e:null,__c:null,constructor:void 0,__v:s??++vx,__i:-1,__u:0};return s==null&&xt.vnode!=null&&xt.vnode(o),o}function gh(i){return i.children}function hh(i,e){this.props=i,this.context=e}function Kr(i,e){if(e==null)return i.__?Kr(i.__,i.__i+1):null;for(var n;e<i.__k.length;e++)if((n=i.__k[e])!=null&&n.__e!=null)return n.__e;return typeof i.type=="function"?Kr(i):null}function AI(i){if(i.__P&&i.__d){var e=i.__v,n=e.__e,r=[],s=[],o=yi({},e);o.__v=e.__v+1,xt.vnode&&xt.vnode(o),Dp(i.__P,o,e,i.__n,i.__P.namespaceURI,32&e.__u?[n]:null,r,n??Kr(e),!!(32&e.__u),s),o.__v=e.__v,o.__.__k[o.__i]=o,Ax(r,o,s),e.__e=e.__=null,o.__e!=n&&Sx(o)}}function Sx(i){if((i=i.__)!=null&&i.__c!=null)return i.__e=i.__c.base=null,i.__k.some(function(e){if(e!=null&&e.__e!=null)return i.__e=i.__c.base=e.__e}),Sx(i)}function mx(i){(!i.__d&&(i.__d=!0)&&yr.push(i)&&!ph.__r++||px!=xt.debounceRendering)&&((px=xt.debounceRendering)||yx)(ph)}function ph(){try{for(var i,e=1;yr.length;)yr.length>e&&yr.sort(xx),i=yr.shift(),e=yr.length,AI(i)}finally{yr.length=ph.__r=0}}function wx(i,e,n,r,s,o,a,l,c,u,h){var d,f,p,g,v,_,m=r&&r.__k||dh,w=e.length;for(c=TI(n,e,m,c,w),d=0;d<w;d++)(p=n.__k[d])!=null&&(f=p.__i!=-1&&m[p.__i]||fh,p.__i=d,_=Dp(i,p,f,s,o,a,l,c,u,h),g=p.__e,p.ref&&f.ref!=p.ref&&(f.ref&&Op(f.ref,null,p),h.push(p.ref,p.__c||g,p)),v==null&&g!=null&&(v=g),4&p.__u?(c=Mx(p,c,i),f.__e&&(f.__e=null)):typeof p.type=="function"&&_!==void 0?c=_:g&&(c=g.nextSibling),p.__u&=-7);return n.__e=v,c}function TI(i,e,n,r,s){var o,a,l,c,u,h=n.length,d=h,f=0;for(i.__k=new Array(s),o=0;o<s;o++)(a=e[o])!=null&&typeof a!="boolean"&&typeof a!="function"?(typeof a=="string"||typeof a=="number"||typeof a=="bigint"||a.constructor==String?a=i.__k[o]=Oa(null,a,null,null,null):mh(a)?a=i.__k[o]=Oa(gh,{children:a},null,null,null):a.constructor===void 0&&a.__b>0?a=i.__k[o]=Oa(a.type,a.props,a.key,a.ref?a.ref:null,a.__v):i.__k[o]=a,c=o+f,a.__=i,a.__b=i.__b+1,l=null,(u=a.__i=CI(a,n,c,d))!=-1&&(d--,(l=n[u])&&(l.__u|=2)),l==null||l.__v==null?(u==-1&&(s>h?f--:s<h&&f++),typeof a.type!="function"&&(a.__u|=4)):u!=c&&(u==c-1?f--:u==c+1?f++:(u>c?f--:f++,a.__u|=4))):i.__k[o]=null;if(d)for(o=0;o<h;o++)(l=n[o])!=null&&(2&l.__u)==0&&(l.__e==r&&(r=Kr(l)),Cx(l,l));return r}function Mx(i,e,n){var r,s;if(typeof i.type=="function"){for(r=i.__k,s=0;r&&s<r.length;s++)r[s]&&(r[s].__=i,e=Mx(r[s],e,n));return e}i.__e!=e&&(e&&i.type&&!e.parentNode&&(e=Kr(i)),e=n.insertBefore(i.__e,e||null));do e=e&&e.nextSibling;while(e!=null&&e.nodeType==8);return e}function CI(i,e,n,r){var s,o,a,l=i.key,c=i.type,u=e[n],h=u!=null&&(2&u.__u)==0;if(u===null&&l==null||h&&l==u.key&&c==u.type)return n;if(r>(h?1:0)){for(s=n-1,o=n+1;s>=0||o<e.length;)if((u=e[a=s>=0?s--:o++])!=null&&(2&u.__u)==0&&l==u.key&&c==u.type)return a}return-1}function gx(i,e,n){e[0]=="-"?i.setProperty(e,n??""):i[e]=n==null?"":typeof n!="number"||MI.test(e)?n:n+"px"}function ch(i,e,n,r,s){var o,a;e:if(e=="style")if(typeof n=="string")i.style.cssText=n;else{if(typeof r=="string"&&(i.style.cssText=r=""),r)for(e in r)n&&e in n||gx(i.style,e,"");if(n)for(e in n)r&&n[e]==r[e]||gx(i.style,e,n[e])}else if(e[0]=="o"&&e[1]=="n")o=e!=(e=e.replace(bx,"$1")),a=e.toLowerCase(),e=a in i||e=="onFocusOut"||e=="onFocusIn"?a.slice(2):e.slice(2),i.l||(i.l={}),i.l[e+o]=n,n?r?n[Da]=r[Da]:(n[Da]=Ip,i.addEventListener(e,o?Rp:Cp,o)):i.removeEventListener(e,o?Rp:Cp,o);else{if(s=="http://www.w3.org/2000/svg")e=e.replace(/xlink(H|:h)/,"h").replace(/sName$/,"s");else if(e!="width"&&e!="height"&&e!="href"&&e!="list"&&e!="form"&&e!="tabIndex"&&e!="download"&&e!="rowSpan"&&e!="colSpan"&&e!="role"&&e!="popover"&&e in i)try{i[e]=n??"";break e}catch{}typeof n=="function"||(n==null||n===!1&&e[4]!="-"?i.removeAttribute(e):i.setAttribute(e,e=="popover"&&n==1?"":n))}}function _x(i){return function(e){if(this.l){var n=this.l[e.type+i];if(e[uh]==null)e[uh]=Ip++;else if(e[uh]<n[Da])return;return n(xt.event?xt.event(e):e)}}}function Dp(i,e,n,r,s,o,a,l,c,u){var h,d,f,p,g,v,_,m,w,T,y,b,S,A,x,C,L=e.type;if(e.constructor!==void 0)return null;128&n.__u&&(c=!!(32&n.__u),o=[l=e.__e=n.__e]),(h=xt.__b)&&h(e);e:if(typeof L=="function"){d=a.length;try{if(w=e.props,T=L.prototype&&L.prototype.render,y=(h=L.contextType)&&r[h.__c],b=h?y?y.props.value:h.__:r,n.__c?m=(f=e.__c=n.__c).__=f.__E:(T?e.__c=f=new L(w,b):(e.__c=f=new hh(w,b),f.constructor=L,f.render=PI),y&&y.sub(f),f.state||(f.state={}),f.__n=r,p=f.__d=!0,f.__h=[],f._sb=[]),T&&f.__s==null&&(f.__s=f.state),T&&L.getDerivedStateFromProps!=null&&(f.__s==f.state&&(f.__s=yi({},f.__s)),yi(f.__s,L.getDerivedStateFromProps(w,f.__s))),g=f.props,v=f.state,f.__v=e,p)T&&L.getDerivedStateFromProps==null&&f.componentWillMount!=null&&f.componentWillMount(),T&&f.componentDidMount!=null&&f.__h.push(f.componentDidMount);else{if(T&&L.getDerivedStateFromProps==null&&w!==g&&f.componentWillReceiveProps!=null&&f.componentWillReceiveProps(w,b),e.__v==n.__v||!f.__e&&f.shouldComponentUpdate!=null&&f.shouldComponentUpdate(w,f.__s,b)===!1){e.__v!=n.__v&&(f.props=w,f.state=f.__s,f.__d=!1),e.__e=n.__e,e.__k=n.__k,e.__k.some(function(P){P&&(P.__=e)}),dh.push.apply(f.__h,f._sb),f._sb=[],f.__h.length&&a.push(f),l=Kr(n);break e}f.componentWillUpdate!=null&&f.componentWillUpdate(w,f.__s,b),T&&f.componentDidUpdate!=null&&f.__h.push(function(){f.componentDidUpdate(g,v,_)})}if(f.context=b,f.props=w,f.__P=i,f.__e=!1,S=xt.__r,A=0,T)f.state=f.__s,f.__d=!1,S&&S(e),h=f.render(f.props,f.state,f.context),dh.push.apply(f.__h,f._sb),f._sb=[];else do f.__d=!1,S&&S(e),h=f.render(f.props,f.state,f.context),f.state=f.__s;while(f.__d&&++A<25);f.state=f.__s,f.getChildContext!=null&&(r=yi(yi({},r),f.getChildContext())),T&&!p&&f.getSnapshotBeforeUpdate!=null&&(_=f.getSnapshotBeforeUpdate(g,v)),x=h!=null&&h.type===gh&&h.key==null?Tx(h.props.children):h,l=wx(i,mh(x)?x:[x],e,n,r,s,o,a,l,c,u),f.base=e.__e,e.__u&=-161,f.__h.length&&a.push(f),m&&(f.__E=f.__=null)}catch(P){if(a.length=d,e.__v=null,c||o!=null){if(P.then){for(e.__u|=c?160:128;l&&l.nodeType==8&&l.nextSibling;)l=l.nextSibling;o!=null&&(o[o.indexOf(l)]=null),e.__e=l}else if(o!=null)for(C=o.length;C--;)Lp(o[C])}else e.__e=n.__e;e.__k==null&&(e.__k=n.__k||[]),P.then||Ex(e),xt.__e(P,e,n)}}else o==null&&e.__v==n.__v?(e.__k=n.__k,e.__e=n.__e):l=e.__e=RI(n.__e,e,n,r,s,o,a,c,u);return(h=xt.diffed)&&h(e),128&e.__u?void 0:l}function Ex(i){i&&(i.__c&&(i.__c.__e=!0),i.__k&&i.__k.some(Ex))}function Ax(i,e,n){for(var r=0;r<n.length;r++)Op(n[r],n[++r],n[++r]);xt.__c&&xt.__c(e,i),i.some(function(s){try{i=s.__h,s.__h=[],i.some(function(o){o.call(s)})}catch(o){xt.__e(o,s.__v)}})}function Tx(i){return typeof i!="object"||i==null||i.__b>0?i:mh(i)?i.map(Tx):i.constructor!==void 0?null:yi({},i)}function RI(i,e,n,r,s,o,a,l,c){var u,h,d,f,p,g,v,_=n.props||fh,m=e.props,w=e.type;if(w=="svg"?s="http://www.w3.org/2000/svg":w=="math"?s="http://www.w3.org/1998/Math/MathML":s||(s="http://www.w3.org/1999/xhtml"),o!=null){for(u=0;u<o.length;u++)if((p=o[u])&&"setAttribute"in p==!!w&&(w?p.localName==w:p.nodeType==3)){i=p,o[u]=null;break}}if(i==null){if(w==null)return document.createTextNode(m);i=document.createElementNS(s,w,m.is&&m),l&&(xt.__m&&xt.__m(e,o),l=!1),o=null}if(w==null)_===m||l&&i.data==m||(i.data=m);else{if(o=w=="textarea"&&m.defaultValue!=null?null:o&&Na.call(i.childNodes),!l&&o!=null)for(_={},u=0;u<i.attributes.length;u++)_[(p=i.attributes[u]).name]=p.value;for(u in _)p=_[u],u=="dangerouslySetInnerHTML"?d=p:u=="children"||u in m||u=="value"&&"defaultValue"in m||u=="checked"&&"defaultChecked"in m||ch(i,u,null,p,s);for(u in m)p=m[u],u=="children"?f=p:u=="dangerouslySetInnerHTML"?h=p:u=="value"?g=p:u=="checked"?v=p:l&&typeof p!="function"||_[u]===p||ch(i,u,p,_[u],s);if(h)l||d&&(h.__html==d.__html||h.__html==i.innerHTML)||(i.innerHTML=h.__html),e.__k=[];else if(d&&(i.innerHTML=""),wx(e.type=="template"?i.content:i,mh(f)?f:[f],e,n,r,w=="foreignObject"?"http://www.w3.org/1999/xhtml":s,o,a,o?o[0]:n.__k&&Kr(n,0),l,c),o!=null)for(u=o.length;u--;)Lp(o[u]);l&&w!="textarea"||(u="value",w=="progress"&&g==null?i.removeAttribute("value"):g!=null&&(g!==i[u]||w=="progress"&&!g||w=="option"&&g!=_[u])&&ch(i,u,g,_[u],s),u="checked",v!=null&&v!=i[u]&&ch(i,u,v,_[u],s))}return i}function Op(i,e,n){try{if(typeof i=="function"){var r=typeof i.__u=="function";r&&i.__u(),r&&e==null||(i.__u=i(e))}else i.current=e}catch(s){xt.__e(s,n)}}function Cx(i,e,n){var r,s;if(xt.unmount&&xt.unmount(i),(r=i.ref)&&(r.current&&r.current!=i.__e||Op(r,null,e)),(r=i.__c)!=null){if(r.componentWillUnmount)try{r.componentWillUnmount()}catch(o){xt.__e(o,e)}r.base=r.__P=r.__n=null}if(r=i.__k)for(s=0;s<r.length;s++)r[s]&&Cx(r[s],e,n||typeof i.type!="function");n||Lp(i.__e),i.__c=i.__=i.__e=void 0}function PI(i,e,n){return this.constructor(i,n)}function Rx(i,e,n){var r,s,o,a;e==document&&(e=document.documentElement),xt.__&&xt.__(i,e),s=(r=typeof n=="function")?null:n&&n.__k||e.__k,o=[],a=[],Dp(e,i=(!r&&n||e).__k=EI(gh,null,[i]),s||fh,fh,e.namespaceURI,!r&&n?[n]:s?null:e.firstChild?Na.call(e.childNodes):null,o,!r&&n?n:s?s.__e:e.firstChild,r,a),Ax(o,i,a),i.props.children=null}function Np(i,e,n){var r,s,o,a,l=yi({},i.props);for(o in i.type&&i.type.defaultProps&&(a=i.type.defaultProps),e)o=="key"?r=e[o]:o=="ref"?s=e[o]:l[o]=e[o]===void 0&&a!=null?a[o]:e[o];return arguments.length>2&&(l.children=arguments.length>3?Na.call(arguments,2):n),Oa(i.type,l,r||i.key,s||i.ref,null)}Na=dh.slice,xt={__e:function(i,e,n,r){for(var s,o,a;e=e.__;)if((s=e.__c)&&!s.__)try{if((o=s.constructor)&&o.getDerivedStateFromError!=null&&(s.setState(o.getDerivedStateFromError(i)),a=s.__d),s.componentDidCatch!=null&&(s.componentDidCatch(i,r||{}),a=s.__d),a)return s.__E=s}catch(l){i=l}throw i}},vx=0,Pp=function(i){return i!=null&&i.constructor===void 0},hh.prototype.setState=function(i,e){var n;n=this.__s!=null&&this.__s!=this.state?this.__s:this.__s=yi({},this.state),typeof i=="function"&&(i=i(yi({},n),this.props)),i&&yi(n,i),i!=null&&this.__v&&(e&&this._sb.push(e),mx(this))},hh.prototype.forceUpdate=function(i){this.__v&&(this.__e=!0,i&&this.__h.push(i),mx(this))},hh.prototype.render=gh,yr=[],yx=typeof Promise=="function"?Promise.prototype.then.bind(Promise.resolve()):setTimeout,xx=function(i,e){return i.__v.__b-e.__v.__b},ph.__r=0,Tp=Math.random().toString(8),uh="__d"+Tp,Da="__a"+Tp,bx=/(PointerCapture)$|Capture$/i,Ip=0,Cp=_x(!1),Rp=_x(!0),wI=0;function Px(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function II(i){if(Array.isArray(i))return i}function LI(i,e,n){return(e=kI(e))in i?Object.defineProperty(i,e,{value:n,enumerable:!0,configurable:!0,writable:!0}):i[e]=n,i}function DI(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function OI(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function Ix(i,e){var n=Object.keys(i);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(i);e&&(r=r.filter(function(s){return Object.getOwnPropertyDescriptor(i,s).enumerable})),n.push.apply(n,r)}return n}function NI(i){for(var e=1;e<arguments.length;e++){var n=arguments[e]!=null?arguments[e]:{};e%2?Ix(Object(n),!0).forEach(function(r){LI(i,r,n[r])}):Object.getOwnPropertyDescriptors?Object.defineProperties(i,Object.getOwnPropertyDescriptors(n)):Ix(Object(n)).forEach(function(r){Object.defineProperty(i,r,Object.getOwnPropertyDescriptor(n,r))})}return i}function UI(i,e){return II(i)||DI(i,e)||BI(i,e)||OI()}function FI(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return(e==="string"?String:Number)(i)}function kI(i){var e=FI(i,"string");return typeof e=="symbol"?e:e+""}function _h(i){"@babel/helpers - typeof";return _h=typeof Symbol=="function"&&typeof Symbol.iterator=="symbol"?function(e){return typeof e}:function(e){return e&&typeof Symbol=="function"&&e.constructor===Symbol&&e!==Symbol.prototype?"symbol":typeof e},_h(i)}function BI(i,e){if(i){if(typeof i=="string")return Px(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Px(i,e):void 0}}var Up=function(e){if(_h(e)!=="object")return e;var n=Np(e);if(n.props){var r;n.props=NI({},n.props),n!=null&&(r=n.props)!==null&&r!==void 0&&r.children&&(n.props.children=Array.isArray(n.props.children)?n.props.children.map(Up):Up(n.props.children))}return n},zI=function(e){return Pp(Np(e))},VI=function(e,n){delete n.__k,Rx(Up(e),n)};function GI(i,e){e===void 0&&(e={});var n=e.insertAt;if(!(typeof document>"u")){var r=document.head||document.getElementsByTagName("head")[0],s=document.createElement("style");s.type="text/css",n==="top"&&r.firstChild?r.insertBefore(s,r.firstChild):r.appendChild(s),s.styleSheet?s.styleSheet.cssText=i:s.appendChild(document.createTextNode(i))}}var HI=`.float-tooltip-kap {
  position: absolute;
  width: max-content; /* prevent shrinking near right edge */
  max-width: max(50%, 150px);
  padding: 3px 5px;
  border-radius: 3px;
  font: 12px sans-serif;
  color: #eee;
  background: rgba(0,0,0,0.6);
  pointer-events: none;
}
`;GI(HI);var vh=ei({props:{content:{default:!1},offsetX:{triggerUpdate:!1},offsetY:{triggerUpdate:!1}},init:function(e,n){var r=arguments.length>2&&arguments[2]!==void 0?arguments[2]:{},s=r.style,o=s===void 0?{}:s,a=!!e&&_h(e)==="object"&&!!e.node&&typeof e.node=="function",l=zt(a?e.node():e);l.style("position")==="static"&&l.style("position","relative"),n.tooltipEl=l.append("div").attr("class","float-tooltip-kap"),Object.entries(o).forEach(function(u){var h=UI(u,2),d=h[0],f=h[1];return n.tooltipEl.style(d,f)}),n.tooltipEl.style("left","-10000px").style("display","none");var c="tooltip-".concat(Math.round(Math.random()*1e12));n.mouseInside=!1,l.on("mousemove.".concat(c),function(u){n.mouseInside=!0;var h=xn(u),d=l.node(),f=d.offsetWidth,p=d.offsetHeight,g=[n.offsetX===null||n.offsetX===void 0?"-".concat(h[0]/f*100,"%"):typeof n.offsetX=="number"?"calc(-50% + ".concat(n.offsetX,"px)"):n.offsetX,n.offsetY===null||n.offsetY===void 0?p>130&&p-h[1]<100?"calc(-100% - 6px)":"21px":typeof n.offsetY=="number"?n.offsetY<0?"calc(-100% - ".concat(Math.abs(n.offsetY),"px)"):"".concat(n.offsetY,"px"):n.offsetY];n.tooltipEl.style("left",h[0]+"px").style("top",h[1]+"px").style("transform","translate(".concat(g.join(","),")")),n.content&&n.tooltipEl.style("display","inline")}),l.on("mouseover.".concat(c),function(){n.mouseInside=!0,n.content&&n.tooltipEl.style("display","inline")}),l.on("mouseout.".concat(c),function(){n.mouseInside=!1,n.tooltipEl.style("display","none")})},update:function(e){e.tooltipEl.style("display",e.content&&e.mouseInside?"inline":"none"),e.content?e.content instanceof HTMLElement?(e.tooltipEl.text(""),e.tooltipEl.append(function(){return e.content})):typeof e.content=="string"?e.tooltipEl.html(e.content):zI(e.content)?(e.tooltipEl.text(""),VI(e.content,e.tooltipEl.node())):(e.tooltipEl.style("display","none"),console.warn("Tooltip content is invalid, skipping.",e.content,e.content.toString())):e.tooltipEl.text("")}});function WI(i,e){e===void 0&&(e={});var n=e.insertAt;if(!(typeof document>"u")){var r=document.head||document.getElementsByTagName("head")[0],s=document.createElement("style");s.type="text/css",n==="top"&&r.firstChild?r.insertBefore(s,r.firstChild):r.appendChild(s),s.styleSheet?s.styleSheet.cssText=i:s.appendChild(document.createTextNode(i))}}var XI=`.scene-nav-info {
  position: absolute;
  bottom: 5px;
  width: 100%;
  text-align: center;
  color: slategrey;
  opacity: 0.7;
  font-size: 10px;
  font-family: sans-serif;
  pointer-events: none;
  user-select: none;
}

.scene-container canvas:focus {
  outline: none;
}`;WI(XI);function Fp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function qI(i){if(Array.isArray(i))return i}function jI(i){if(Array.isArray(i))return Fp(i)}function YI(i,e,n){return(e=t3(e))in i?Object.defineProperty(i,e,{value:n,enumerable:!0,configurable:!0,writable:!0}):i[e]=n,i}function $I(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function ZI(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function KI(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function JI(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function QI(i,e){return qI(i)||ZI(i,e)||Lx(i,e)||KI()}function Jr(i){return jI(i)||$I(i)||Lx(i)||JI()}function e3(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return(e==="string"?String:Number)(i)}function t3(i){var e=e3(i,"string");return typeof e=="symbol"?e:e+""}function Lx(i,e){if(i){if(typeof i=="string")return Fp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Fp(i,e):void 0}}var Dx=function(e){e instanceof Array?e.forEach(Dx):(e.map&&e.map.dispose(),e.dispose())},Ox=function(e){e.geometry&&e.geometry.dispose(),e.material&&Dx(e.material),e.texture&&e.texture.dispose(),e.children&&e.children.forEach(Ox)},n3=function(e){for(;e.children.length;){var n=e.children[0];e.remove(n),Ox(n)}},bt=window.THREE?window.THREE:{WebGLRenderer:su,Scene:Ao,PerspectiveCamera:$t,Raycaster:Nr,SRGBColorSpace:on,TextureLoader:ko,Vector2:fe,Vector3:F,Box3:In,Color:We,Mesh:kt,SphereGeometry:Dr,MeshBasicMaterial:Ir,BackSide:Gt,Timer:Or},kp=ei({props:{width:{default:window.innerWidth,onChange:function(e,n,r){isNaN(e)&&(n.width=r)}},height:{default:window.innerHeight,onChange:function(e,n,r){isNaN(e)&&(n.height=r)}},viewOffset:{default:[0,0]},backgroundColor:{default:"#000011"},backgroundImageUrl:{},onBackgroundImageLoaded:{},showNavInfo:{default:!0},skyRadius:{default:5e4},objects:{default:[]},lights:{default:[]},enablePointerInteraction:{default:!0,onChange:function(e,n){n.hoverObj=null,n.tooltip&&n.tooltip.content(null)},triggerUpdate:!1},pointerRaycasterThrottleMs:{default:50,triggerUpdate:!1},lineHoverPrecision:{default:1,triggerUpdate:!1},pointsHoverPrecision:{default:1,triggerUpdate:!1},hoverOrderComparator:{triggerUpdate:!1},hoverFilter:{default:function(){return!0},triggerUpdate:!1},tooltipContent:{triggerUpdate:!1},hoverDuringDrag:{default:!1,triggerUpdate:!1},clickAfterDrag:{default:!1,triggerUpdate:!1},onHover:{default:function(){},triggerUpdate:!1},onClick:{default:function(){},triggerUpdate:!1},onRightClick:{triggerUpdate:!1}},methods:{tick:function(e){if(e.initialised){e.controls.enabled&&e.controls.update&&e.controls.update(Math.min(1,e.timer.update().getDelta())),e.postProcessingComposer?e.postProcessingComposer.render():e.renderer.render(e.scene,e.camera),e.extraRenderers.forEach(function(a){return a.render(e.scene,e.camera)});var n=+new Date;if(e.enablePointerInteraction&&n-e.lastRaycasterCheck>=e.pointerRaycasterThrottleMs){e.lastRaycasterCheck=n;var r=null;if(e.hoverDuringDrag||!e.isPointerDragging){var s=this.intersectingObjects(e.pointerPos.x,e.pointerPos.y);e.hoverOrderComparator&&s.sort(function(a,l){return e.hoverOrderComparator(a.object,l.object)});var o=s.find(function(a){return e.hoverFilter(a.object)})||null;r=o?o.object:null,e.intersection=o||null}r!==e.hoverObj&&(e.onHover(r,e.hoverObj,e.intersection),e.tooltip.content(r&&we(e.tooltipContent)(r,e.intersection)||null),e.hoverObj=r)}e.tweenGroup.update()}return this},getPointerPos:function(e){var n=e.pointerPos,r=n.x,s=n.y;return{x:r,y:s}},cameraPosition:function(e,n,r,s){var o=e.camera;if(n&&e.initialised){var a,l,c=n,u=r||{x:0,y:0,z:0};if((a=e.povPosTween)===null||a===void 0||a.end(),(l=e.povTgtTween)===null||l===void 0||l.end(),!s)f(c),p(u);else{var h=Object.assign({},o.position),d=g();e.tweenGroup.add(e.povPosTween=new oo(h).to(c,s).easing(si.Quadratic.Out).onUpdate(f).onComplete(function(){e.povPosTween=void 0,e.tweenGroup.remove(this)}).start()),e.tweenGroup.add(e.povTgtTween=new oo(d).to(u,s/3).easing(si.Quadratic.Out).onUpdate(p).onComplete(function(){e.povTgtTween=void 0,e.tweenGroup.remove(this)}).start())}return this}return Object.assign({},o.position,{lookAt:g()});function f(v){var _=v.x,m=v.y,w=v.z;_!==void 0&&(o.position.x=_),m!==void 0&&(o.position.y=m),w!==void 0&&(o.position.z=w)}function p(v){var _=new bt.Vector3(v.x,v.y,v.z);e.controls.enabled&&e.controls.target?e.controls.target=_:o.lookAt(_)}function g(){return Object.assign(new bt.Vector3(0,0,-1e3).applyQuaternion(o.quaternion).add(o.position))}},zoomToFit:function(e){for(var n=arguments.length>1&&arguments[1]!==void 0?arguments[1]:0,r=arguments.length>2&&arguments[2]!==void 0?arguments[2]:10,s=arguments.length,o=new Array(s>3?s-3:0),a=3;a<s;a++)o[a-3]=arguments[a];return this.fitToBbox(this.getBbox.apply(this,o),n,r)},fitToBbox:function(e,n){var r=arguments.length>2&&arguments[2]!==void 0?arguments[2]:0,s=arguments.length>3&&arguments[3]!==void 0?arguments[3]:10,o=e.camera;if(n){var a=new bt.Vector3(0,0,0),l=Math.max.apply(Math,Jr(Object.entries(n).map(function(p){var g=QI(p,2),v=g[0],_=g[1];return Math.max.apply(Math,Jr(_.map(function(m){return Math.abs(a[v]-m)})))})))*2,c=(1-s*2/e.height)*o.fov,u=l/Math.atan(c*Math.PI/180),h=u/o.aspect,d=Math.max(u,h);if(d>0){var f=a.clone().sub(o.position).normalize().multiplyScalar(-d);this.cameraPosition(f,a,r)}}return this},getBbox:function(e){var n=arguments.length>1&&arguments[1]!==void 0?arguments[1]:function(){return!0},r=new bt.Box3(new bt.Vector3(0,0,0),new bt.Vector3(0,0,0)),s=e.objects.filter(n);return s.length?(s.forEach(function(o){return r.expandByObject(o)}),Object.assign.apply(Object,Jr(["x","y","z"].map(function(o){return YI({},o,[r.min[o],r.max[o]])})))):null},getScreenCoords:function(e,n,r,s){var o=new bt.Vector3(n,r,s);return o.project(this.camera()),{x:(o.x+1)*e.width/2,y:-(o.y-1)*e.height/2}},getSceneCoords:function(e,n,r){var s=arguments.length>3&&arguments[3]!==void 0?arguments[3]:0,o=new bt.Vector2(n/e.width*2-1,-(r/e.height)*2+1),a=new bt.Raycaster;return a.setFromCamera(o,e.camera),Object.assign({},a.ray.at(s,new bt.Vector3))},intersectingObjects:function(e,n,r){var s=new bt.Vector2(n/e.width*2-1,-(r/e.height)*2+1),o=new bt.Raycaster;return o.params.Line.threshold=e.lineHoverPrecision,o.params.Points.threshold=e.pointsHoverPrecision,o.setFromCamera(s,e.camera),o.intersectObjects(e.objects,!0)},renderer:function(e){return e.renderer},scene:function(e){return e.scene},camera:function(e){return e.camera},postProcessingComposer:function(e){return e.postProcessingComposer},controls:function(e){return e.controls},tbControls:function(e){return e.controls},_destructor:function(e){var n,r,s;n3(e.scene),(n=e.controls)===null||n===void 0||n.dispose(),(r=e.renderer)===null||r===void 0||r.dispose(),(s=e.postProcessingComposer)===null||s===void 0||s.dispose()}},stateInit:function(){return{scene:new bt.Scene,camera:new bt.PerspectiveCamera,timer:new bt.Timer,tweenGroup:new Ra,lastRaycasterCheck:0}},init:function(e,n){var r=arguments.length>2&&arguments[2]!==void 0?arguments[2]:{},s=r.controlType,o=s===void 0?"trackball":s,a=r.useWebGPU,l=a===void 0?!1:a,c=r.rendererConfig,u=c===void 0?{}:c,h=r.extraRenderers,d=h===void 0?[]:h,f=r.waitForLoadComplete,p=f===void 0?!0:f;e.innerHTML="",e.appendChild(n.container=document.createElement("div")),n.container.className="scene-container",n.container.style.position="relative",n.container.appendChild(n.navInfo=document.createElement("div")),n.navInfo.className="scene-nav-info",n.navInfo.textContent={orbit:"Left-click: rotate, Mouse-wheel/middle-click: zoom, Right-click: pan",trackball:"Left-click: rotate, Mouse-wheel/middle-click: zoom, Right-click: pan",fly:"WASD: move, R|F: up | down, Q|E: roll, up|down: pitch, left|right: yaw"}[o]||"",n.navInfo.style.display=n.showNavInfo?null:"none",n.tooltip=new vh(n.container),n.pointerPos=new bt.Vector2,n.pointerPos.x=-2,n.pointerPos.y=-2,["pointermove","pointerdown"].forEach(function(g){return n.container.addEventListener(g,function(v){if(g==="pointerdown"&&(n.isPointerPressed=!0),!n.isPointerDragging&&v.type==="pointermove"&&(v.pressure>0||n.isPointerPressed)&&(v.pointerType==="mouse"||v.movementX===void 0||[v.movementX,v.movementY].some(function(w){return Math.abs(w)>1}))&&(n.isPointerDragging=!0),n.enablePointerInteraction){var _=m(n.container);n.pointerPos.x=v.pageX-_.left,n.pointerPos.y=v.pageY-_.top}function m(w){var T=w.getBoundingClientRect(),y=window.pageXOffset||document.documentElement.scrollLeft,b=window.pageYOffset||document.documentElement.scrollTop;return{top:T.top+b,left:T.left+y}}},{passive:!0})}),n.container.addEventListener("pointerup",function(g){n.isPointerPressed&&(n.isPointerPressed=!1,!(n.isPointerDragging&&(n.isPointerDragging=!1,!n.clickAfterDrag))&&requestAnimationFrame(function(){g.button===0&&n.onClick(n.hoverObj||null,g,n.intersection),g.button===2&&n.onRightClick&&n.onRightClick(n.hoverObj||null,g,n.intersection)}))},{passive:!0,capture:!0}),n.container.addEventListener("contextmenu",function(g){n.onRightClick&&g.preventDefault()}),n.renderer=new(l?ku:bt.WebGLRenderer)(Object.assign({antialias:!0,alpha:!0},u)),n.renderer.setPixelRatio(Math.min(2,window.devicePixelRatio)),n.container.appendChild(n.renderer.domElement),n.extraRenderers=d,n.extraRenderers.forEach(function(g){g.domElement.style.position="absolute",g.domElement.style.top="0px",g.domElement.style.pointerEvents="none",n.container.appendChild(g.domElement)}),n.postProcessingComposer=new Ju(n.renderer),n.postProcessingComposer.addPass(new Qu(n.scene,n.camera)),n.controls=new{trackball:Xu,orbit:ju,fly:Yu}[o](n.camera,n.renderer.domElement),o==="fly"&&(n.controls.movementSpeed=300,n.controls.rollSpeed=Math.PI/6,n.controls.dragToLook=!0),(o==="trackball"||o==="orbit")&&(n.controls.minDistance=.1,n.controls.maxDistance=n.skyRadius,n.controls.addEventListener("start",function(){n.controlsEngaged=!0}),n.controls.addEventListener("change",function(){n.controlsEngaged&&(n.controlsDragging=!0)}),n.controls.addEventListener("end",function(){n.controlsEngaged=!1,n.controlsDragging=!1})),[n.renderer,n.postProcessingComposer].concat(Jr(n.extraRenderers)).forEach(function(g){return g.setSize(n.width,n.height)}),n.camera.aspect=n.width/n.height,n.camera.updateProjectionMatrix(),n.camera.position.z=1e3,n.scene.add(n.skysphere=new bt.Mesh),n.skysphere.visible=!1,n.loadComplete=n.scene.visible=!p,window.scene=n.scene},update:function(e,n){if(e.width&&e.height&&(n.hasOwnProperty("width")||n.hasOwnProperty("height"))){var r,s=e.width,o=e.height;e.container.style.width="".concat(s,"px"),e.container.style.height="".concat(o,"px"),[e.renderer,e.postProcessingComposer].concat(Jr(e.extraRenderers)).forEach(function(p){return p.setSize(s,o)}),e.camera.aspect=s/o;var a=e.viewOffset.slice(0,2);a.some(function(p){return p})&&(r=e.camera).setViewOffset.apply(r,[s,o].concat(Jr(a),[s,o])),e.camera.updateProjectionMatrix()}if(n.hasOwnProperty("viewOffset")){var l,c=e.width,u=e.height,h=e.viewOffset.slice(0,2);h.some(function(p){return p})?(l=e.camera).setViewOffset.apply(l,[c,u].concat(Jr(h),[c,u])):e.camera.clearViewOffset()}if(n.hasOwnProperty("skyRadius")&&e.skyRadius&&(e.controls.hasOwnProperty("maxDistance")&&n.skyRadius&&(e.controls.maxDistance=Math.min(e.controls.maxDistance,e.skyRadius)),e.camera.far=e.skyRadius*2.5,e.camera.updateProjectionMatrix(),e.skysphere.geometry=new bt.SphereGeometry(e.skyRadius)),n.hasOwnProperty("backgroundColor")){var d=mr(e.backgroundColor).alpha;d===void 0&&(d=1),e.renderer.setClearColor(new bt.Color(My(1,e.backgroundColor)),d)}n.hasOwnProperty("backgroundImageUrl")&&(e.backgroundImageUrl?new bt.TextureLoader().load(e.backgroundImageUrl,function(p){p.colorSpace=bt.SRGBColorSpace,e.skysphere.material=new bt.MeshBasicMaterial({map:p,side:bt.BackSide}),e.skysphere.visible=!0,e.onBackgroundImageLoaded&&setTimeout(e.onBackgroundImageLoaded),!e.loadComplete&&f()}):(e.skysphere.visible=!1,e.skysphere.material.map=null,!e.loadComplete&&f())),n.hasOwnProperty("showNavInfo")&&(e.navInfo.style.display=e.showNavInfo?null:"none"),n.hasOwnProperty("lights")&&((n.lights||[]).forEach(function(p){return e.scene.remove(p)}),e.lights.forEach(function(p){return e.scene.add(p)})),n.hasOwnProperty("objects")&&((n.objects||[]).forEach(function(p){return e.scene.remove(p)}),e.objects.forEach(function(p){return e.scene.add(p)}));function f(){e.loadComplete=e.scene.visible=!0}}});function i3(i,e){e===void 0&&(e={});var n=e.insertAt;if(!(typeof document>"u")){var r=document.head||document.getElementsByTagName("head")[0],s=document.createElement("style");s.type="text/css",n==="top"&&r.firstChild?r.insertBefore(s,r.firstChild):r.appendChild(s),s.styleSheet?s.styleSheet.cssText=i:s.appendChild(document.createTextNode(i))}}var r3=`.graph-info-msg {
  top: 50%;
  width: 100%;
  text-align: center;
  color: lavender;
  opacity: 0.7;
  font-size: 22px;
  position: absolute;
  font-family: Sans-serif;
}

.scene-container .clickable {
  cursor: pointer;
}

.scene-container .grabbable {
  cursor: move;
  cursor: grab;
  cursor: -moz-grab;
  cursor: -webkit-grab;
}

.scene-container .grabbable:active {
  cursor: grabbing;
  cursor: -moz-grabbing;
  cursor: -webkit-grabbing;
}`;i3(r3);function zp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function s3(i){if(Array.isArray(i))return zp(i)}function Ua(i,e,n){return(e=c3(e))in i?Object.defineProperty(i,e,{value:n,enumerable:!0,configurable:!0,writable:!0}):i[e]=n,i}function o3(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function a3(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function Nx(i,e){var n=Object.keys(i);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(i);e&&(r=r.filter(function(s){return Object.getOwnPropertyDescriptor(i,s).enumerable})),n.push.apply(n,r)}return n}function yh(i){for(var e=1;e<arguments.length;e++){var n=arguments[e]!=null?arguments[e]:{};e%2?Nx(Object(n),!0).forEach(function(r){Ua(i,r,n[r])}):Object.getOwnPropertyDescriptors?Object.defineProperties(i,Object.getOwnPropertyDescriptors(n)):Nx(Object(n)).forEach(function(r){Object.defineProperty(i,r,Object.getOwnPropertyDescriptor(n,r))})}return i}function bh(i){return s3(i)||o3(i)||u3(i)||a3()}function l3(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return(e==="string"?String:Number)(i)}function c3(i){var e=l3(i,"string");return typeof e=="symbol"?e:e+""}function u3(i,e){if(i){if(typeof i=="string")return zp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?zp(i,e):void 0}}function Ux(i,e){var n=new e;return n._destructor&&n._destructor(),{linkProp:function(s){return{default:n[s](),onChange:function(a,l){l[i][s](a)},triggerUpdate:!1}},linkMethod:function(s){return function(o){for(var a=o[i],l=arguments.length,c=new Array(l>1?l-1:0),u=1;u<l;u++)c[u-1]=arguments[u];var h=a[s].apply(a,c);return h===a?this:h}}}}var Bp=window.THREE?window.THREE:{AmbientLight:Go,DirectionalLight:Vo,REVISION:"186"},h3=170,Fx=Ux("forceGraph",ap),f3=Object.assign.apply(Object,bh(["jsonUrl","graphData","numDimensions","dagMode","dagLevelDistance","dagNodeFilter","onDagError","nodeRelSize","nodeId","nodeVal","nodeResolution","nodeColor","nodeAutoColorBy","nodeOpacity","nodeVisibility","nodeThreeObject","nodeThreeObjectExtend","nodePositionUpdate","linkSource","linkTarget","linkVisibility","linkColor","linkAutoColorBy","linkOpacity","linkWidth","linkResolution","linkCurvature","linkCurveRotation","linkMaterial","linkThreeObject","linkThreeObjectExtend","linkPositionUpdate","linkDirectionalArrowLength","linkDirectionalArrowColor","linkDirectionalArrowRelPos","linkDirectionalArrowResolution","linkDirectionalParticles","linkDirectionalParticleSpeed","linkDirectionalParticleOffset","linkDirectionalParticleWidth","linkDirectionalParticleColor","linkDirectionalParticleResolution","linkDirectionalParticleThreeObject","forceEngine","d3AlphaDecay","d3VelocityDecay","d3AlphaMin","ngraphPhysics","warmupTicks","cooldownTicks","cooldownTime","onEngineTick","onEngineStop"].map(function(i){return Ua({},i,Fx.linkProp(i))}))),d3=Object.assign.apply(Object,bh(["refresh","getGraphBbox","d3Force","d3ReheatSimulation","emitParticle"].map(function(i){return Ua({},i,Fx.linkMethod(i))}))),xh=Ux("renderObjs",kp),p3=Object.assign.apply(Object,bh(["width","height","backgroundColor","showNavInfo","enablePointerInteraction"].map(function(i){return Ua({},i,xh.linkProp(i))}))),m3=Object.assign.apply(Object,bh(["lights","cameraPosition","postProcessingComposer"].map(function(i){return Ua({},i,xh.linkMethod(i))})).concat([{graph2ScreenCoords:xh.linkMethod("getScreenCoords"),screen2GraphCoords:xh.linkMethod("getSceneCoords")}])),kx=ei({props:yh(yh({nodeLabel:{default:"name",triggerUpdate:!1},linkLabel:{default:"name",triggerUpdate:!1},linkHoverPrecision:{default:1,onChange:function(e,n){return n.renderObjs.lineHoverPrecision(e)},triggerUpdate:!1},enableNavigationControls:{default:!0,onChange:function(e,n){var r=n.renderObjs.controls();r&&(r.enabled=e,e&&r.domElement&&r.domElement.dispatchEvent(new PointerEvent("pointerup")))},triggerUpdate:!1},enableNodeDrag:{default:!0,triggerUpdate:!1},onNodeDrag:{default:function(){},triggerUpdate:!1},onNodeDragEnd:{default:function(){},triggerUpdate:!1},onNodeClick:{triggerUpdate:!1},onNodeRightClick:{triggerUpdate:!1},onNodeHover:{triggerUpdate:!1},onLinkClick:{triggerUpdate:!1},onLinkRightClick:{triggerUpdate:!1},onLinkHover:{triggerUpdate:!1},onBackgroundClick:{triggerUpdate:!1},onBackgroundRightClick:{triggerUpdate:!1},showPointerCursor:{default:!0,triggerUpdate:!1}},f3),p3),methods:yh(yh({zoomToFit:function(e,n,r){for(var s,o=arguments.length,a=new Array(o>3?o-3:0),l=3;l<o;l++)a[l-3]=arguments[l];return e.renderObjs.fitToBbox((s=e.forceGraph).getGraphBbox.apply(s,a),n,r),this},pauseAnimation:function(e){return e.animationFrameRequestId!==null&&(cancelAnimationFrame(e.animationFrameRequestId),e.animationFrameRequestId=null),this},resumeAnimation:function(e){return e.animationFrameRequestId===null&&this._animationCycle(),this},_animationCycle:function(e){e.enablePointerInteraction&&(this.renderer().domElement.style.cursor=null),e.forceGraph.tickFrame(),e.renderObjs.tick(),e.animationFrameRequestId=requestAnimationFrame(this._animationCycle)},scene:function(e){return e.renderObjs.scene()},camera:function(e){return e.renderObjs.camera()},renderer:function(e){return e.renderObjs.renderer()},controls:function(e){return e.renderObjs.controls()},_destructor:function(e){var n,r;this.pauseAnimation(),this.graphData({nodes:[],links:[]}),(n=(r=e.forceGraph)._destructor)===null||n===void 0||n.call(r),e.renderObjs._destructor()}},d3),m3),stateInit:function(e){var n=e.controlType,r=e.rendererConfig,s=e.extraRenderers,o=new ap;return{forceGraph:o,renderObjs:kp({controlType:n,rendererConfig:r,extraRenderers:s}).objects([o]).lights([new Bp.AmbientLight(13421772,Math.PI),new Bp.DirectionalLight(16777215,.6*Math.PI)])}},init:function(e,n){e.innerHTML="",e.appendChild(n.container=document.createElement("div")),n.container.style.position="relative";var r=document.createElement("div");n.container.appendChild(r),n.renderObjs(r);var s=n.renderObjs.camera(),o=n.renderObjs.renderer(),a=n.renderObjs.controls();a.enabled=!!n.enableNavigationControls,n.lastSetCameraZ=s.position.z;var l;n.container.appendChild(l=document.createElement("div")),l.className="graph-info-msg",l.textContent="",n.forceGraph.onLoading(function(){l.textContent="Loading..."}).onFinishLoading(function(){l.textContent=""}).onUpdate(function(){n.graphData=n.forceGraph.graphData(),s.position.x===0&&s.position.y===0&&s.position.z===n.lastSetCameraZ&&n.graphData.nodes.length&&(s.lookAt(n.forceGraph.position),n.lastSetCameraZ=s.position.z=Math.cbrt(n.graphData.nodes.length)*h3)}).onFinishUpdate(function(){if(n._dragControls){var c=n.graphData.nodes.find(function(h){return h.__initialFixedPos&&!h.__disposeControlsAfterDrag});c?c.__disposeControlsAfterDrag=!0:n._dragControls.dispose(),n._dragControls=void 0}if(n.enableNodeDrag&&n.enablePointerInteraction&&n.forceEngine==="d3"){var u=n._dragControls=new uu(n.graphData.nodes.map(function(h){return h.__threeObj}).filter(function(h){return h}),s,o.domElement);u.addEventListener("dragstart",function(h){var d=ki(h.object);if(d){a.enabled=!1,h.object.__initialPos=h.object.position.clone(),h.object.__prevPos=h.object.position.clone();var f=d.__data;!f.__initialFixedPos&&(f.__initialFixedPos={fx:f.fx,fy:f.fy,fz:f.fz}),!f.__initialPos&&(f.__initialPos={x:f.x,y:f.y,z:f.z}),["x","y","z"].forEach(function(p){return f["f".concat(p)]=f[p]}),o.domElement.classList.add("grabbable")}}),u.addEventListener("drag",function(h){var d=ki(h.object);if(d){if(!h.object.hasOwnProperty("__graphObjType")){var f=h.object.__initialPos,p=h.object.__prevPos,g=h.object.position;d.position.add(g.clone().sub(p)),p.copy(g),g.copy(f)}var v=d.__data,_=d.position,m={x:_.x-v.x,y:_.y-v.y,z:_.z-v.z};["x","y","z"].forEach(function(w){return v["f".concat(w)]=v[w]=_[w]}),n.forceGraph.d3AlphaTarget(.3).resetCountdown(),v.__dragged=!0,n.onNodeDrag(v,m)}}),u.addEventListener("dragend",function(h){var d=ki(h.object);if(d){delete h.object.__initialPos,delete h.object.__prevPos;var f=d.__data;f.__disposeControlsAfterDrag&&(u.dispose(),delete f.__disposeControlsAfterDrag);var p=f.__initialFixedPos,g=f.__initialPos,v={x:g.x-f.x,y:g.y-f.y,z:g.z-f.z};if(p&&(["x","y","z"].forEach(function(m){var w="f".concat(m);p[w]===void 0&&delete f[w]}),delete f.__initialFixedPos,delete f.__initialPos,f.__dragged&&(delete f.__dragged,n.onNodeDragEnd(f,v))),n.forceGraph.d3AlphaTarget(0).resetCountdown(),n.enableNavigationControls){var _;a.enabled=!0,a._status&&((_=a._onPointerCancel)===null||_===void 0||_.call(a)),a.domElement&&a.domElement.ownerDocument&&a.domElement.ownerDocument.dispatchEvent(new PointerEvent("pointerup",{pointerType:"touch"}))}o.domElement.classList.remove("grabbable")}})}}),Bp.REVISION<155&&(n.renderObjs.renderer().useLegacyLights=!1),n.renderObjs.hoverOrderComparator(function(c,u){var h=ki(c);if(!h)return 1;var d=ki(u);if(!d)return-1;var f=function(g){return g.__graphObjType==="node"};return f(d)-f(h)}).tooltipContent(function(c){var u=ki(c);return u&&we(n["".concat(u.__graphObjType,"Label")])(u.__data)||""}).hoverDuringDrag(!1).onHover(function(c){var u=ki(c);if(u!==n.hoverObj){var h=n.hoverObj?n.hoverObj.__graphObjType:null,d=n.hoverObj?n.hoverObj.__data:null,f=u?u.__graphObjType:null,p=u?u.__data:null;if(h&&h!==f){var g=n["on".concat(h==="node"?"Node":"Link","Hover")];g&&g(null,d)}if(f){var v=n["on".concat(f==="node"?"Node":"Link","Hover")];v&&v(p,h===f?d:null)}o.domElement.classList[(u&&n["on".concat(f==="node"?"Node":"Link","Click")]||!u&&n.onBackgroundClick)&&we(n.showPointerCursor)(p)?"add":"remove"]("clickable"),n.hoverObj=u}}).clickAfterDrag(!1).onClick(function(c,u){var h=ki(c);if(h){var d=n["on".concat(h.__graphObjType==="node"?"Node":"Link","Click")];d&&d(h.__data,u)}else n.onBackgroundClick&&n.onBackgroundClick(u)}).onRightClick(function(c,u){var h=ki(c);if(h){var d=n["on".concat(h.__graphObjType==="node"?"Node":"Link","RightClick")];d&&d(h.__data,u)}else n.onBackgroundRightClick&&n.onBackgroundRightClick(u)}),this._animationCycle()}});function ki(i){for(var e=i;e&&!e.hasOwnProperty("__graphObjType");)e=e.parent;return e}var Bx={passive:!1},Qr={capture:!0,passive:!1};function Sh(i){i.stopImmediatePropagation()}function xr(i){i.preventDefault(),i.stopImmediatePropagation()}function Fa(i){var e=i.document.documentElement,n=zt(i).on("dragstart.drag",xr,Qr);"onselectstart"in e?n.on("selectstart.drag",xr,Qr):(e.__noselect=e.style.MozUserSelect,e.style.MozUserSelect="none")}function ka(i,e){var n=i.document.documentElement,r=zt(i).on("dragstart.drag",null);e&&(r.on("click.drag",xr,Qr),setTimeout(function(){r.on("click.drag",null)},0)),"onselectstart"in n?r.on("selectstart.drag",null):(n.style.MozUserSelect=n.__noselect,delete n.__noselect)}var Ba=i=>()=>i;function za(i,{sourceEvent:e,subject:n,target:r,identifier:s,active:o,x:a,y:l,dx:c,dy:u,dispatch:h}){Object.defineProperties(this,{type:{value:i,enumerable:!0,configurable:!0},sourceEvent:{value:e,enumerable:!0,configurable:!0},subject:{value:n,enumerable:!0,configurable:!0},target:{value:r,enumerable:!0,configurable:!0},identifier:{value:s,enumerable:!0,configurable:!0},active:{value:o,enumerable:!0,configurable:!0},x:{value:a,enumerable:!0,configurable:!0},y:{value:l,enumerable:!0,configurable:!0},dx:{value:c,enumerable:!0,configurable:!0},dy:{value:u,enumerable:!0,configurable:!0},_:{value:h}})}za.prototype.on=function(){var i=this._.on.apply(this._,arguments);return i===this._?this:i};function g3(i){return!i.ctrlKey&&!i.button}function _3(){return this.parentNode}function v3(i,e){return e??{x:i.x,y:i.y}}function y3(){return navigator.maxTouchPoints||"ontouchstart"in this}function Vp(){var i=g3,e=_3,n=v3,r=y3,s={},o=Di("start","drag","end"),a=0,l,c,u,h,d=0;function f(y){y.on("mousedown.drag",p).filter(r).on("touchstart.drag",_).on("touchmove.drag",m,Bx).on("touchend.drag touchcancel.drag",w).style("touch-action","none").style("-webkit-tap-highlight-color","rgba(0,0,0,0)")}function p(y,b){if(!(h||!i.call(this,y,b))){var S=T(this,e.call(this,y,b),y,b,"mouse");S&&(zt(y.view).on("mousemove.drag",g,Qr).on("mouseup.drag",v,Qr),Fa(y.view),Sh(y),u=!1,l=y.clientX,c=y.clientY,S("start",y))}}function g(y){if(xr(y),!u){var b=y.clientX-l,S=y.clientY-c;u=b*b+S*S>d}s.mouse("drag",y)}function v(y){zt(y.view).on("mousemove.drag mouseup.drag",null),ka(y.view,u),xr(y),s.mouse("end",y)}function _(y,b){if(i.call(this,y,b)){var S=y.changedTouches,A=e.call(this,y,b),x=S.length,C,L;for(C=0;C<x;++C)(L=T(this,A,y,b,S[C].identifier,S[C]))&&(Sh(y),L("start",y,S[C]))}}function m(y){var b=y.changedTouches,S=b.length,A,x;for(A=0;A<S;++A)(x=s[b[A].identifier])&&(xr(y),x("drag",y,b[A]))}function w(y){var b=y.changedTouches,S=b.length,A,x;for(h&&clearTimeout(h),h=setTimeout(function(){h=null},500),A=0;A<S;++A)(x=s[b[A].identifier])&&(Sh(y),x("end",y,b[A]))}function T(y,b,S,A,x,C){var L=o.copy(),P=xn(C||S,b),O,U,M;if((M=n.call(y,new za("beforestart",{sourceEvent:S,target:f,identifier:x,active:a,x:P[0],y:P[1],dx:0,dy:0,dispatch:L}),A))!=null)return O=M.x-P[0]||0,U=M.y-P[1]||0,function I(D,k,q){var W=P,j;switch(D){case"start":s[x]=I,j=a++;break;case"end":delete s[x],--a;case"drag":P=xn(q||k,b),j=a;break}L.call(D,y,new za(D,{sourceEvent:k,subject:M,target:f,identifier:x,active:j,x:P[0]+O,y:P[1]+U,dx:P[0]-W[0],dy:P[1]-W[1],dispatch:L}),A)}}return f.filter=function(y){return arguments.length?(i=typeof y=="function"?y:Ba(!!y),f):i},f.container=function(y){return arguments.length?(e=typeof y=="function"?y:Ba(y),f):e},f.subject=function(y){return arguments.length?(n=typeof y=="function"?y:Ba(y),f):n},f.touchable=function(y){return arguments.length?(r=typeof y=="function"?y:Ba(!!y),f):r},f.on=function(){var y=o.on.apply(o,arguments);return y===o?f:y},f.clickDistance=function(y){return arguments.length?(d=(y=+y)*y,f):Math.sqrt(d)},f}var x3=Di("start","end","cancel","interrupt"),b3=[],Gx=0,zx=1,Mh=2,wh=3,Vx=4,Eh=5,Va=6;function br(i,e,n,r,s,o){var a=i.__transition;if(!a)i.__transition={};else if(n in a)return;S3(i,n,{name:e,index:r,group:s,on:x3,tween:b3,time:o.time,delay:o.delay,duration:o.duration,ease:o.ease,timer:null,state:Gx})}function Ga(i,e){var n=Lt(i,e);if(n.state>Gx)throw new Error("too late; already scheduled");return n}function Wt(i,e){var n=Lt(i,e);if(n.state>wh)throw new Error("too late; already running");return n}function Lt(i,e){var n=i.__transition;if(!n||!(n=n[e]))throw new Error("transition not found");return n}function S3(i,e,n){var r=i.__transition,s;r[e]=n,n.timer=$s(o,0,n.time);function o(u){n.state=zx,n.timer.restart(a,n.delay,n.time),n.delay<=u&&a(u-n.delay)}function a(u){var h,d,f,p;if(n.state!==zx)return c();for(h in r)if(p=r[h],p.name===n.name){if(p.state===wh)return mu(a);p.state===Vx?(p.state=Va,p.timer.stop(),p.on.call("interrupt",i,i.__data__,p.index,p.group),delete r[h]):+h<e&&(p.state=Va,p.timer.stop(),p.on.call("cancel",i,i.__data__,p.index,p.group),delete r[h])}if(mu(function(){n.state===wh&&(n.state=Vx,n.timer.restart(l,n.delay,n.time),l(u))}),n.state=Mh,n.on.call("start",i,i.__data__,n.index,n.group),n.state===Mh){for(n.state=wh,s=new Array(f=n.tween.length),h=0,d=-1;h<f;++h)(p=n.tween[h].value.call(i,i.__data__,n.index,n.group))&&(s[++d]=p);s.length=d+1}}function l(u){for(var h=u<n.duration?n.ease.call(null,u/n.duration):(n.timer.restart(c),n.state=Eh,1),d=-1,f=s.length;++d<f;)s[d].call(i,h);n.state===Eh&&(n.on.call("end",i,i.__data__,n.index,n.group),c())}function c(){n.state=Va,n.timer.stop(),delete r[e];for(var u in r)return;delete i.__transition}}function es(i,e){var n=i.__transition,r,s,o=!0,a;if(n){e=e==null?null:e+"";for(a in n){if((r=n[a]).name!==e){o=!1;continue}s=r.state>Mh&&r.state<Eh,r.state=Va,r.timer.stop(),r.on.call(s?"interrupt":"cancel",i,i.__data__,r.index,r.group),delete n[a]}o&&delete i.__transition}}function Hx(i){return this.each(function(){es(this,i)})}function w3(i,e){var n,r;return function(){var s=Wt(this,i),o=s.tween;if(o!==n){r=n=o;for(var a=0,l=r.length;a<l;++a)if(r[a].name===e){r=r.slice(),r.splice(a,1);break}}s.tween=r}}function M3(i,e,n){var r,s;if(typeof n!="function")throw new Error;return function(){var o=Wt(this,i),a=o.tween;if(a!==r){s=(r=a).slice();for(var l={name:e,value:n},c=0,u=s.length;c<u;++c)if(s[c].name===e){s[c]=l;break}c===u&&s.push(l)}o.tween=s}}function Wx(i,e){var n=this._id;if(i+="",arguments.length<2){for(var r=Lt(this.node(),n).tween,s=0,o=r.length,a;s<o;++s)if((a=r[s]).name===i)return a.value;return null}return this.each((e==null?w3:M3)(n,i,e))}function ao(i,e,n){var r=i._id;return i.each(function(){var s=Wt(this,r);(s.value||(s.value={}))[e]=n.apply(this,arguments)}),function(s){return Lt(s,r).value[e]}}function Ah(i,e){var n;return(typeof e=="number"?kn:e instanceof hr?Pu:(n=hr(e))?(e=n,Pu):Wd)(i,e)}function E3(i){return function(){this.removeAttribute(i)}}function A3(i){return function(){this.removeAttributeNS(i.space,i.local)}}function T3(i,e,n){var r,s=n+"",o;return function(){var a=this.getAttribute(i);return a===s?null:a===r?o:o=e(r=a,n)}}function C3(i,e,n){var r,s=n+"",o;return function(){var a=this.getAttributeNS(i.space,i.local);return a===s?null:a===r?o:o=e(r=a,n)}}function R3(i,e,n){var r,s,o;return function(){var a,l=n(this),c;return l==null?void this.removeAttribute(i):(a=this.getAttribute(i),c=l+"",a===c?null:a===r&&c===s?o:(s=c,o=e(r=a,l)))}}function P3(i,e,n){var r,s,o;return function(){var a,l=n(this),c;return l==null?void this.removeAttributeNS(i.space,i.local):(a=this.getAttributeNS(i.space,i.local),c=l+"",a===c?null:a===r&&c===s?o:(s=c,o=e(r=a,l)))}}function Xx(i,e){var n=Ui(i),r=n==="transform"?jd:Ah;return this.attrTween(i,typeof e=="function"?(n.local?P3:R3)(n,r,ao(this,"attr."+i,e)):e==null?(n.local?A3:E3)(n):(n.local?C3:T3)(n,r,e))}function I3(i,e){return function(n){this.setAttribute(i,e.call(this,n))}}function L3(i,e){return function(n){this.setAttributeNS(i.space,i.local,e.call(this,n))}}function D3(i,e){var n,r;function s(){var o=e.apply(this,arguments);return o!==r&&(n=(r=o)&&L3(i,o)),n}return s._value=e,s}function O3(i,e){var n,r;function s(){var o=e.apply(this,arguments);return o!==r&&(n=(r=o)&&I3(i,o)),n}return s._value=e,s}function qx(i,e){var n="attr."+i;if(arguments.length<2)return(n=this.tween(n))&&n._value;if(e==null)return this.tween(n,null);if(typeof e!="function")throw new Error;var r=Ui(i);return this.tween(n,(r.local?D3:O3)(r,e))}function N3(i,e){return function(){Ga(this,i).delay=+e.apply(this,arguments)}}function U3(i,e){return e=+e,function(){Ga(this,i).delay=e}}function jx(i){var e=this._id;return arguments.length?this.each((typeof i=="function"?N3:U3)(e,i)):Lt(this.node(),e).delay}function F3(i,e){return function(){Wt(this,i).duration=+e.apply(this,arguments)}}function k3(i,e){return e=+e,function(){Wt(this,i).duration=e}}function Yx(i){var e=this._id;return arguments.length?this.each((typeof i=="function"?F3:k3)(e,i)):Lt(this.node(),e).duration}function B3(i,e){if(typeof e!="function")throw new Error;return function(){Wt(this,i).ease=e}}function $x(i){var e=this._id;return arguments.length?this.each(B3(e,i)):Lt(this.node(),e).ease}function z3(i,e){return function(){var n=e.apply(this,arguments);if(typeof n!="function")throw new Error;Wt(this,i).ease=n}}function Zx(i){if(typeof i!="function")throw new Error;return this.each(z3(this._id,i))}function Kx(i){typeof i!="function"&&(i=Ia(i));for(var e=this._groups,n=e.length,r=new Array(n),s=0;s<n;++s)for(var o=e[s],a=o.length,l=r[s]=[],c,u=0;u<a;++u)(c=o[u])&&i.call(c,c.__data__,u,o)&&l.push(c);return new hn(r,this._parents,this._name,this._id)}function Jx(i){if(i._id!==this._id)throw new Error;for(var e=this._groups,n=i._groups,r=e.length,s=n.length,o=Math.min(r,s),a=new Array(r),l=0;l<o;++l)for(var c=e[l],u=n[l],h=c.length,d=a[l]=new Array(h),f,p=0;p<h;++p)(f=c[p]||u[p])&&(d[p]=f);for(;l<r;++l)a[l]=e[l];return new hn(a,this._parents,this._name,this._id)}function V3(i){return(i+"").trim().split(/^|\s+/).every(function(e){var n=e.indexOf(".");return n>=0&&(e=e.slice(0,n)),!e||e==="start"})}function G3(i,e,n){var r,s,o=V3(e)?Ga:Wt;return function(){var a=o(this,i),l=a.on;l!==r&&(s=(r=l).copy()).on(e,n),a.on=s}}function Qx(i,e){var n=this._id;return arguments.length<2?Lt(this.node(),n).on.on(i):this.each(G3(n,i,e))}function H3(i){return function(){var e=this.parentNode;for(var n in this.__transition)if(+n!==i)return;e&&e.removeChild(this)}}function eb(){return this.on("end.remove",H3(this._id))}function tb(i){var e=this._name,n=this._id;typeof i!="function"&&(i=Zr(i));for(var r=this._groups,s=r.length,o=new Array(s),a=0;a<s;++a)for(var l=r[a],c=l.length,u=o[a]=new Array(c),h,d,f=0;f<c;++f)(h=l[f])&&(d=i.call(h,h.__data__,f,l))&&("__data__"in h&&(d.__data__=h.__data__),u[f]=d,br(u[f],e,n,f,u,Lt(h,n)));return new hn(o,this._parents,e,n)}function nb(i){var e=this._name,n=this._id;typeof i!="function"&&(i=Pa(i));for(var r=this._groups,s=r.length,o=[],a=[],l=0;l<s;++l)for(var c=r[l],u=c.length,h,d=0;d<u;++d)if(h=c[d]){for(var f=i.call(h,h.__data__,d,c),p,g=Lt(h,n),v=0,_=f.length;v<_;++v)(p=f[v])&&br(p,e,n,v,f,g);o.push(f),a.push(h)}return new hn(o,a,e,n)}var W3=Fi.prototype.constructor;function ib(){return new W3(this._groups,this._parents)}function X3(i,e){var n,r,s;return function(){var o=vr(this,i),a=(this.style.removeProperty(i),vr(this,i));return o===a?null:o===n&&a===r?s:s=e(n=o,r=a)}}function rb(i){return function(){this.style.removeProperty(i)}}function q3(i,e,n){var r,s=n+"",o;return function(){var a=vr(this,i);return a===s?null:a===r?o:o=e(r=a,n)}}function j3(i,e,n){var r,s,o;return function(){var a=vr(this,i),l=n(this),c=l+"";return l==null&&(c=l=(this.style.removeProperty(i),vr(this,i))),a===c?null:a===r&&c===s?o:(s=c,o=e(r=a,l))}}function Y3(i,e){var n,r,s,o="style."+e,a="end."+o,l;return function(){var c=Wt(this,i),u=c.on,h=c.value[o]==null?l||(l=rb(e)):void 0;(u!==n||s!==h)&&(r=(n=u).copy()).on(a,s=h),c.on=r}}function sb(i,e,n){var r=(i+="")=="transform"?qd:Ah;return e==null?this.styleTween(i,X3(i,r)).on("end.style."+i,rb(i)):typeof e=="function"?this.styleTween(i,j3(i,r,ao(this,"style."+i,e))).each(Y3(this._id,i)):this.styleTween(i,q3(i,r,e),n).on("end.style."+i,null)}function $3(i,e,n){return function(r){this.style.setProperty(i,e.call(this,r),n)}}function Z3(i,e,n){var r,s;function o(){var a=e.apply(this,arguments);return a!==s&&(r=(s=a)&&$3(i,a,n)),r}return o._value=e,o}function ob(i,e,n){var r="style."+(i+="");if(arguments.length<2)return(r=this.tween(r))&&r._value;if(e==null)return this.tween(r,null);if(typeof e!="function")throw new Error;return this.tween(r,Z3(i,e,n??""))}function K3(i){return function(){this.textContent=i}}function J3(i){return function(){var e=i(this);this.textContent=e??""}}function ab(i){return this.tween("text",typeof i=="function"?J3(ao(this,"text",i)):K3(i==null?"":i+""))}function Q3(i){return function(e){this.textContent=i.call(this,e)}}function eL(i){var e,n;function r(){var s=i.apply(this,arguments);return s!==n&&(e=(n=s)&&Q3(s)),e}return r._value=i,r}function lb(i){var e="text";if(arguments.length<1)return(e=this.tween(e))&&e._value;if(i==null)return this.tween(e,null);if(typeof i!="function")throw new Error;return this.tween(e,eL(i))}function cb(){for(var i=this._name,e=this._id,n=Th(),r=this._groups,s=r.length,o=0;o<s;++o)for(var a=r[o],l=a.length,c,u=0;u<l;++u)if(c=a[u]){var h=Lt(c,e);br(c,i,n,u,a,{time:h.time+h.delay+h.duration,delay:0,duration:h.duration,ease:h.ease})}return new hn(r,this._parents,i,n)}function ub(){var i,e,n=this,r=n._id,s=n.size();return new Promise(function(o,a){var l={value:a},c={value:function(){--s===0&&o()}};n.each(function(){var u=Wt(this,r),h=u.on;h!==i&&(e=(i=h).copy(),e._.cancel.push(l),e._.interrupt.push(l),e._.end.push(c)),u.on=e}),s===0&&o()})}var tL=0;function hn(i,e,n,r){this._groups=i,this._parents=e,this._name=n,this._id=r}function hb(i){return Fi().transition(i)}function Th(){return++tL}var Bi=Fi.prototype;hn.prototype=hb.prototype={constructor:hn,select:tb,selectAll:nb,selectChild:Bi.selectChild,selectChildren:Bi.selectChildren,filter:Kx,merge:Jx,selection:ib,transition:cb,call:Bi.call,nodes:Bi.nodes,node:Bi.node,size:Bi.size,empty:Bi.empty,each:Bi.each,on:Qx,attr:Xx,attrTween:qx,style:sb,styleTween:ob,text:ab,textTween:lb,remove:eb,tween:Wx,delay:jx,duration:Yx,ease:$x,easeVarying:Zx,end:ub,[Symbol.iterator]:Bi[Symbol.iterator]};function Ch(i){return((i*=2)<=1?i*i*i:(i-=2)*i*i+2)/2}var nL={time:null,delay:0,duration:250,ease:Ch};function iL(i,e){for(var n;!(n=i.__transition)||!(n=n[e]);)if(!(i=i.parentNode))throw new Error(`transition ${e} not found`);return n}function fb(i){var e,n;i instanceof hn?(e=i._id,i=i._name):(e=Th(),(n=nL).time=ha(),i=i==null?null:i+"");for(var r=this._groups,s=r.length,o=0;o<s;++o)for(var a=r[o],l=a.length,c,u=0;u<l;++u)(c=a[u])&&br(c,i,e,u,a,n||iL(c,e));return new hn(r,this._parents,i,e)}Fi.prototype.interrupt=Hx;Fi.prototype.transition=fb;var Ha=i=>()=>i;function Gp(i,{sourceEvent:e,target:n,transform:r,dispatch:s}){Object.defineProperties(this,{type:{value:i,enumerable:!0,configurable:!0},sourceEvent:{value:e,enumerable:!0,configurable:!0},target:{value:n,enumerable:!0,configurable:!0},transform:{value:r,enumerable:!0,configurable:!0},_:{value:s}})}function oi(i,e,n){this.k=i,this.x=e,this.y=n}oi.prototype={constructor:oi,scale:function(i){return i===1?this:new oi(this.k*i,this.x,this.y)},translate:function(i,e){return i===0&e===0?this:new oi(this.k,this.x+this.k*i,this.y+this.k*e)},apply:function(i){return[i[0]*this.k+this.x,i[1]*this.k+this.y]},applyX:function(i){return i*this.k+this.x},applyY:function(i){return i*this.k+this.y},invert:function(i){return[(i[0]-this.x)/this.k,(i[1]-this.y)/this.k]},invertX:function(i){return(i-this.x)/this.k},invertY:function(i){return(i-this.y)/this.k},rescaleX:function(i){return i.copy().domain(i.range().map(this.invertX,this).map(i.invert,i))},rescaleY:function(i){return i.copy().domain(i.range().map(this.invertY,this).map(i.invert,i))},toString:function(){return"translate("+this.x+","+this.y+") scale("+this.k+")"}};var Wa=new oi(1,0,0);Cn.prototype=oi.prototype;function Cn(i){for(;!i.__zoom;)if(!(i=i.parentNode))return Wa;return i.__zoom}function Rh(i){i.stopImmediatePropagation()}function lo(i){i.preventDefault(),i.stopImmediatePropagation()}function rL(i){return(!i.ctrlKey||i.type==="wheel")&&!i.button}function sL(){var i=this;return i instanceof SVGElement?(i=i.ownerSVGElement||i,i.hasAttribute("viewBox")?(i=i.viewBox.baseVal,[[i.x,i.y],[i.x+i.width,i.y+i.height]]):[[0,0],[i.width.baseVal.value,i.height.baseVal.value]]):[[0,0],[i.clientWidth,i.clientHeight]]}function db(){return this.__zoom||Wa}function oL(i){return-i.deltaY*(i.deltaMode===1?.05:i.deltaMode?1:.002)*(i.ctrlKey?10:1)}function aL(){return navigator.maxTouchPoints||"ontouchstart"in this}function lL(i,e,n){var r=i.invertX(e[0][0])-n[0][0],s=i.invertX(e[1][0])-n[1][0],o=i.invertY(e[0][1])-n[0][1],a=i.invertY(e[1][1])-n[1][1];return i.translate(s>r?(r+s)/2:Math.min(0,r)||Math.max(0,s),a>o?(o+a)/2:Math.min(0,o)||Math.max(0,a))}function Hp(){var i=rL,e=sL,n=lL,r=oL,s=aL,o=[0,1/0],a=[[-1/0,-1/0],[1/0,1/0]],l=250,c=Yd,u=Di("start","zoom","end"),h,d,f,p=500,g=150,v=0,_=10;function m(M){M.property("__zoom",db).on("wheel.zoom",x,{passive:!1}).on("mousedown.zoom",C).on("dblclick.zoom",L).filter(s).on("touchstart.zoom",P).on("touchmove.zoom",O).on("touchend.zoom touchcancel.zoom",U).style("-webkit-tap-highlight-color","rgba(0,0,0,0)")}m.transform=function(M,I,D,k){var q=M.selection?M.selection():M;q.property("__zoom",db),M!==q?b(M,I,D,k):q.interrupt().each(function(){S(this,arguments).event(k).start().zoom(null,typeof I=="function"?I.apply(this,arguments):I).end()})},m.scaleBy=function(M,I,D,k){m.scaleTo(M,function(){var q=this.__zoom.k,W=typeof I=="function"?I.apply(this,arguments):I;return q*W},D,k)},m.scaleTo=function(M,I,D,k){m.transform(M,function(){var q=e.apply(this,arguments),W=this.__zoom,j=D==null?y(q):typeof D=="function"?D.apply(this,arguments):D,B=W.invert(j),Q=typeof I=="function"?I.apply(this,arguments):I;return n(T(w(W,Q),j,B),q,a)},D,k)},m.translateBy=function(M,I,D,k){m.transform(M,function(){return n(this.__zoom.translate(typeof I=="function"?I.apply(this,arguments):I,typeof D=="function"?D.apply(this,arguments):D),e.apply(this,arguments),a)},null,k)},m.translateTo=function(M,I,D,k,q){m.transform(M,function(){var W=e.apply(this,arguments),j=this.__zoom,B=k==null?y(W):typeof k=="function"?k.apply(this,arguments):k;return n(Wa.translate(B[0],B[1]).scale(j.k).translate(typeof I=="function"?-I.apply(this,arguments):-I,typeof D=="function"?-D.apply(this,arguments):-D),W,a)},k,q)};function w(M,I){return I=Math.max(o[0],Math.min(o[1],I)),I===M.k?M:new oi(I,M.x,M.y)}function T(M,I,D){var k=I[0]-D[0]*M.k,q=I[1]-D[1]*M.k;return k===M.x&&q===M.y?M:new oi(M.k,k,q)}function y(M){return[(+M[0][0]+ +M[1][0])/2,(+M[0][1]+ +M[1][1])/2]}function b(M,I,D,k){M.on("start.zoom",function(){S(this,arguments).event(k).start()}).on("interrupt.zoom end.zoom",function(){S(this,arguments).event(k).end()}).tween("zoom",function(){var q=this,W=arguments,j=S(q,W).event(k),B=e.apply(q,W),Q=D==null?y(B):typeof D=="function"?D.apply(q,W):D,re=Math.max(B[1][0]-B[0][0],B[1][1]-B[0][1]),be=q.__zoom,ee=typeof I=="function"?I.apply(q,W):I,oe=c(be.invert(Q).concat(re/be.k),ee.invert(Q).concat(re/ee.k));return function(V){if(V===1)V=ee;else{var Z=oe(V),ce=re/Z[2];V=new oi(ce,Q[0]-Z[0]*ce,Q[1]-Z[1]*ce)}j.zoom(null,V)}})}function S(M,I,D){return!D&&M.__zooming||new A(M,I)}function A(M,I){this.that=M,this.args=I,this.active=0,this.sourceEvent=null,this.extent=e.apply(M,I),this.taps=0}A.prototype={event:function(M){return M&&(this.sourceEvent=M),this},start:function(){return++this.active===1&&(this.that.__zooming=this,this.emit("start")),this},zoom:function(M,I){return this.mouse&&M!=="mouse"&&(this.mouse[1]=I.invert(this.mouse[0])),this.touch0&&M!=="touch"&&(this.touch0[1]=I.invert(this.touch0[0])),this.touch1&&M!=="touch"&&(this.touch1[1]=I.invert(this.touch1[0])),this.that.__zoom=I,this.emit("zoom"),this},end:function(){return--this.active===0&&(delete this.that.__zooming,this.emit("end")),this},emit:function(M){var I=zt(this.that).datum();u.call(M,this.that,new Gp(M,{sourceEvent:this.sourceEvent,target:m,type:M,transform:this.that.__zoom,dispatch:u}),I)}};function x(M,...I){if(!i.apply(this,arguments))return;var D=S(this,I).event(M),k=this.__zoom,q=Math.max(o[0],Math.min(o[1],k.k*Math.pow(2,r.apply(this,arguments)))),W=xn(M);if(D.wheel)(D.mouse[0][0]!==W[0]||D.mouse[0][1]!==W[1])&&(D.mouse[1]=k.invert(D.mouse[0]=W)),clearTimeout(D.wheel);else{if(k.k===q)return;D.mouse=[W,k.invert(W)],es(this),D.start()}lo(M),D.wheel=setTimeout(j,g),D.zoom("mouse",n(T(w(k,q),D.mouse[0],D.mouse[1]),D.extent,a));function j(){D.wheel=null,D.end()}}function C(M,...I){if(f||!i.apply(this,arguments))return;var D=M.currentTarget,k=S(this,I,!0).event(M),q=zt(M.view).on("mousemove.zoom",Q,!0).on("mouseup.zoom",re,!0),W=xn(M,D),j=M.clientX,B=M.clientY;Fa(M.view),Rh(M),k.mouse=[W,this.__zoom.invert(W)],es(this),k.start();function Q(be){if(lo(be),!k.moved){var ee=be.clientX-j,oe=be.clientY-B;k.moved=ee*ee+oe*oe>v}k.event(be).zoom("mouse",n(T(k.that.__zoom,k.mouse[0]=xn(be,D),k.mouse[1]),k.extent,a))}function re(be){q.on("mousemove.zoom mouseup.zoom",null),ka(be.view,k.moved),lo(be),k.event(be).end()}}function L(M,...I){if(i.apply(this,arguments)){var D=this.__zoom,k=xn(M.changedTouches?M.changedTouches[0]:M,this),q=D.invert(k),W=D.k*(M.shiftKey?.5:2),j=n(T(w(D,W),k,q),e.apply(this,I),a);lo(M),l>0?zt(this).transition().duration(l).call(b,j,k,M):zt(this).call(m.transform,j,k,M)}}function P(M,...I){if(i.apply(this,arguments)){var D=M.touches,k=D.length,q=S(this,I,M.changedTouches.length===k).event(M),W,j,B,Q;for(Rh(M),j=0;j<k;++j)B=D[j],Q=xn(B,this),Q=[Q,this.__zoom.invert(Q),B.identifier],q.touch0?!q.touch1&&q.touch0[2]!==Q[2]&&(q.touch1=Q,q.taps=0):(q.touch0=Q,W=!0,q.taps=1+!!h);h&&(h=clearTimeout(h)),W&&(q.taps<2&&(d=Q[0],h=setTimeout(function(){h=null},p)),es(this),q.start())}}function O(M,...I){if(this.__zooming){var D=S(this,I).event(M),k=M.changedTouches,q=k.length,W,j,B,Q;for(lo(M),W=0;W<q;++W)j=k[W],B=xn(j,this),D.touch0&&D.touch0[2]===j.identifier?D.touch0[0]=B:D.touch1&&D.touch1[2]===j.identifier&&(D.touch1[0]=B);if(j=D.that.__zoom,D.touch1){var re=D.touch0[0],be=D.touch0[1],ee=D.touch1[0],oe=D.touch1[1],V=(V=ee[0]-re[0])*V+(V=ee[1]-re[1])*V,Z=(Z=oe[0]-be[0])*Z+(Z=oe[1]-be[1])*Z;j=w(j,Math.sqrt(V/Z)),B=[(re[0]+ee[0])/2,(re[1]+ee[1])/2],Q=[(be[0]+oe[0])/2,(be[1]+oe[1])/2]}else if(D.touch0)B=D.touch0[0],Q=D.touch0[1];else return;D.zoom("touch",n(T(j,B,Q),D.extent,a))}}function U(M,...I){if(this.__zooming){var D=S(this,I).event(M),k=M.changedTouches,q=k.length,W,j;for(Rh(M),f&&clearTimeout(f),f=setTimeout(function(){f=null},p),W=0;W<q;++W)j=k[W],D.touch0&&D.touch0[2]===j.identifier?delete D.touch0:D.touch1&&D.touch1[2]===j.identifier&&delete D.touch1;if(D.touch1&&!D.touch0&&(D.touch0=D.touch1,delete D.touch1),D.touch0)D.touch0[1]=this.__zoom.invert(D.touch0[0]);else if(D.end(),D.taps===2&&(j=xn(j,this),Math.hypot(d[0]-j[0],d[1]-j[1])<_)){var B=zt(this).on("dblclick.zoom");B&&B.apply(this,arguments)}}}return m.wheelDelta=function(M){return arguments.length?(r=typeof M=="function"?M:Ha(+M),m):r},m.filter=function(M){return arguments.length?(i=typeof M=="function"?M:Ha(!!M),m):i},m.touchable=function(M){return arguments.length?(s=typeof M=="function"?M:Ha(!!M),m):s},m.extent=function(M){return arguments.length?(e=typeof M=="function"?M:Ha([[+M[0][0],+M[0][1]],[+M[1][0],+M[1][1]]]),m):e},m.scaleExtent=function(M){return arguments.length?(o[0]=+M[0],o[1]=+M[1],m):[o[0],o[1]]},m.translateExtent=function(M){return arguments.length?(a[0][0]=+M[0][0],a[1][0]=+M[1][0],a[0][1]=+M[0][1],a[1][1]=+M[1][1],m):[[a[0][0],a[0][1]],[a[1][0],a[1][1]]]},m.constrain=function(M){return arguments.length?(n=M,m):n},m.duration=function(M){return arguments.length?(l=+M,m):l},m.interpolate=function(M){return arguments.length?(c=M,m):c},m.on=function(){var M=u.on.apply(u,arguments);return M===u?m:M},m.clickDistance=function(M){return arguments.length?(v=(M=+M)*M,m):Math.sqrt(v)},m.tapDistance=function(M){return arguments.length?(_=+M,m):_},m}var cL="Expected a function";function uL(i,e,n){var r=!0,s=!0;if(typeof i!="function")throw new TypeError(cL);return Hr(n)&&(r="leading"in n?!!n.leading:r,s="trailing"in n?!!n.trailing:s),bu(i,e,{leading:r,maxWait:e,trailing:s})}var Wp=uL;function Xp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function hL(i){if(Array.isArray(i))return Xp(i)}function _b(i,e,n){if(typeof i=="function"?i===e:i.has(e))return arguments.length<3?e:n;throw new TypeError("Private element is not present on this object")}function fL(i,e){if(e.has(i))throw new TypeError("Cannot initialize the same private elements twice on an object")}function dL(i,e){if(!(i instanceof e))throw new TypeError("Cannot call a class as a function")}function Bn(i,e){return i.get(_b(i,e))}function pb(i,e,n){fL(i,e),e.set(i,n)}function mb(i,e,n){return i.set(_b(i,e),n),n}function pL(i,e){for(var n=0;n<e.length;n++){var r=e[n];r.enumerable=r.enumerable||!1,r.configurable=!0,"value"in r&&(r.writable=!0),Object.defineProperty(i,xL(r.key),r)}}function mL(i,e,n){return e&&pL(i.prototype,e),Object.defineProperty(i,"prototype",{writable:!1}),i}function gL(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function _L(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function vL(i){return hL(i)||gL(i)||bL(i)||_L()}function yL(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return String(i)}function xL(i){var e=yL(i,"string");return typeof e=="symbol"?e:e+""}function bL(i,e){if(i){if(typeof i=="string")return Xp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Xp(i,e):void 0}}var SL=123,wL=function(e){return"#".concat(Math.min(e,Math.pow(2,24)).toString(16).padStart(6,"0"))},vb=function(e,n,r){return(e<<16)+(n<<8)+r},ML=function(e){var n=Pe(e).toRgb(),r=n.r,s=n.g,o=n.b;return vb(r,s,o)},gb=function(e,n){return e*SL%Math.pow(2,n)},ts=new WeakMap,zi=new WeakMap,yb=(function(){function i(){var e=arguments.length>0&&arguments[0]!==void 0?arguments[0]:6;dL(this,i),pb(this,ts,void 0),pb(this,zi,void 0),mb(zi,this,e),this.reset()}return mL(i,[{key:"reset",value:function(){mb(ts,this,["__reserved for background__"])}},{key:"register",value:function(n){if(Bn(ts,this).length>=Math.pow(2,24-Bn(zi,this)))return null;var r=Bn(ts,this).length,s=gb(r,Bn(zi,this)),o=wL(r+(s<<24-Bn(zi,this)));return Bn(ts,this).push(n),o}},{key:"lookup",value:function(n){if(!n)return null;var r=typeof n=="string"?ML(n):vb.apply(void 0,vL(n));if(!r)return null;var s=r&Math.pow(2,24-Bn(zi,this))-1,o=r>>24-Bn(zi,this)&Math.pow(2,Bn(zi,this))-1;return gb(s,Bn(zi,this))!==o||s>=Bn(ts,this).length?null:Bn(ts,this)[s]}}])})();var{abs:Xa,cos:Vi,sin:co,acos:EL,atan2:qa,sqrt:Sr,pow:zn}=Math;function ja(i){return i<0?-zn(-i,1/3):zn(i,1/3)}var xb=Math.PI,Ph=2*xb,wr=xb/2,AL=1e-6,qp=Number.MAX_SAFE_INTEGER||9007199254740991,jp=Number.MIN_SAFE_INTEGER||-9007199254740991,TL={x:0,y:0,z:0},le={Tvalues:[-.06405689286260563,.06405689286260563,-.1911188674736163,.1911188674736163,-.3150426796961634,.3150426796961634,-.4337935076260451,.4337935076260451,-.5454214713888396,.5454214713888396,-.6480936519369755,.6480936519369755,-.7401241915785544,.7401241915785544,-.820001985973903,.820001985973903,-.8864155270044011,.8864155270044011,-.9382745520027328,.9382745520027328,-.9747285559713095,.9747285559713095,-.9951872199970213,.9951872199970213],Cvalues:[.12793819534675216,.12793819534675216,.1258374563468283,.1258374563468283,.12167047292780339,.12167047292780339,.1155056680537256,.1155056680537256,.10744427011596563,.10744427011596563,.09761865210411388,.09761865210411388,.08619016153195327,.08619016153195327,.0733464814110803,.0733464814110803,.05929858491543678,.05929858491543678,.04427743881741981,.04427743881741981,.028531388628933663,.028531388628933663,.0123412297999872,.0123412297999872],arcfn:function(i,e){let n=e(i),r=n.x*n.x+n.y*n.y;return typeof n.z<"u"&&(r+=n.z*n.z),Sr(r)},compute:function(i,e,n){if(i===0)return e[0].t=0,e[0];let r=e.length-1;if(i===1)return e[r].t=1,e[r];let s=1-i,o=e;if(r===0)return e[0].t=i,e[0];if(r===1){let l={x:s*o[0].x+i*o[1].x,y:s*o[0].y+i*o[1].y,t:i};return n&&(l.z=s*o[0].z+i*o[1].z),l}if(r<4){let l=s*s,c=i*i,u,h,d,f=0;r===2?(o=[o[0],o[1],o[2],TL],u=l,h=s*i*2,d=c):r===3&&(u=l*s,h=l*i*3,d=s*c*3,f=i*c);let p={x:u*o[0].x+h*o[1].x+d*o[2].x+f*o[3].x,y:u*o[0].y+h*o[1].y+d*o[2].y+f*o[3].y,t:i};return n&&(p.z=u*o[0].z+h*o[1].z+d*o[2].z+f*o[3].z),p}let a=JSON.parse(JSON.stringify(e));for(;a.length>1;){for(let l=0;l<a.length-1;l++)a[l]={x:a[l].x+(a[l+1].x-a[l].x)*i,y:a[l].y+(a[l+1].y-a[l].y)*i},typeof a[l].z<"u"&&(a[l].z=a[l].z+(a[l+1].z-a[l].z)*i);a.splice(a.length-1,1)}return a[0].t=i,a[0]},computeWithRatios:function(i,e,n,r){let s=1-i,o=n,a=e,l=o[0],c=o[1],u=o[2],h=o[3],d;if(l*=s,c*=i,a.length===2)return d=l+c,{x:(l*a[0].x+c*a[1].x)/d,y:(l*a[0].y+c*a[1].y)/d,z:r?(l*a[0].z+c*a[1].z)/d:!1,t:i};if(l*=s,c*=2*s,u*=i*i,a.length===3)return d=l+c+u,{x:(l*a[0].x+c*a[1].x+u*a[2].x)/d,y:(l*a[0].y+c*a[1].y+u*a[2].y)/d,z:r?(l*a[0].z+c*a[1].z+u*a[2].z)/d:!1,t:i};if(l*=s,c*=1.5*s,u*=3*s,h*=i*i*i,a.length===4)return d=l+c+u+h,{x:(l*a[0].x+c*a[1].x+u*a[2].x+h*a[3].x)/d,y:(l*a[0].y+c*a[1].y+u*a[2].y+h*a[3].y)/d,z:r?(l*a[0].z+c*a[1].z+u*a[2].z+h*a[3].z)/d:!1,t:i}},derive:function(i,e){let n=[];for(let r=i,s=r.length,o=s-1;s>1;s--,o--){let a=[];for(let l=0,c;l<o;l++)c={x:o*(r[l+1].x-r[l].x),y:o*(r[l+1].y-r[l].y)},e&&(c.z=o*(r[l+1].z-r[l].z)),a.push(c);n.push(a),r=a}return n},between:function(i,e,n){return e<=i&&i<=n||le.approximately(i,e)||le.approximately(i,n)},approximately:function(i,e,n){return Xa(i-e)<=(n||AL)},length:function(i){let n=le.Tvalues.length,r=0;for(let s=0,o;s<n;s++)o=.5*le.Tvalues[s]+.5,r+=le.Cvalues[s]*le.arcfn(o,i);return .5*r},map:function(i,e,n,r,s){let o=n-e,a=s-r,l=i-e,c=l/o;return r+a*c},lerp:function(i,e,n){let r={x:e.x+i*(n.x-e.x),y:e.y+i*(n.y-e.y)};return e.z!==void 0&&n.z!==void 0&&(r.z=e.z+i*(n.z-e.z)),r},pointToString:function(i){let e=i.x+"/"+i.y;return typeof i.z<"u"&&(e+="/"+i.z),e},pointsToString:function(i){return"["+i.map(le.pointToString).join(", ")+"]"},copy:function(i){return JSON.parse(JSON.stringify(i))},angle:function(i,e,n){let r=e.x-i.x,s=e.y-i.y,o=n.x-i.x,a=n.y-i.y,l=r*a-s*o,c=r*o+s*a;return qa(l,c)},round:function(i,e){let n=""+i,r=n.indexOf(".");return parseFloat(n.substring(0,r+1+e))},dist:function(i,e){let n=i.x-e.x,r=i.y-e.y;return Sr(n*n+r*r)},closest:function(i,e){let n=zn(2,63),r,s;return i.forEach(function(o,a){s=le.dist(e,o),s<n&&(n=s,r=a)}),{mdist:n,mpos:r}},abcratio:function(i,e){if(e!==2&&e!==3)return!1;if(typeof i>"u")i=.5;else if(i===0||i===1)return i;let n=zn(i,e)+zn(1-i,e),r=n-1;return Xa(r/n)},projectionratio:function(i,e){if(e!==2&&e!==3)return!1;if(typeof i>"u")i=.5;else if(i===0||i===1)return i;let n=zn(1-i,e),r=zn(i,e)+n;return n/r},lli8:function(i,e,n,r,s,o,a,l){let c=(i*r-e*n)*(s-a)-(i-n)*(s*l-o*a),u=(i*r-e*n)*(o-l)-(e-r)*(s*l-o*a),h=(i-n)*(o-l)-(e-r)*(s-a);return h==0?!1:{x:c/h,y:u/h}},lli4:function(i,e,n,r){let s=i.x,o=i.y,a=e.x,l=e.y,c=n.x,u=n.y,h=r.x,d=r.y;return le.lli8(s,o,a,l,c,u,h,d)},lli:function(i,e){return le.lli4(i,i.c,e,e.c)},makeline:function(i,e){return new ns(i.x,i.y,(i.x+e.x)/2,(i.y+e.y)/2,e.x,e.y)},findbbox:function(i){let e=qp,n=qp,r=jp,s=jp;return i.forEach(function(o){let a=o.bbox();e>a.x.min&&(e=a.x.min),n>a.y.min&&(n=a.y.min),r<a.x.max&&(r=a.x.max),s<a.y.max&&(s=a.y.max)}),{x:{min:e,mid:(e+r)/2,max:r,size:r-e},y:{min:n,mid:(n+s)/2,max:s,size:s-n}}},shapeintersections:function(i,e,n,r,s){if(!le.bboxoverlap(e,r))return[];let o=[],a=[i.startcap,i.forward,i.back,i.endcap],l=[n.startcap,n.forward,n.back,n.endcap];return a.forEach(function(c){c.virtual||l.forEach(function(u){if(u.virtual)return;let h=c.intersects(u,s);h.length>0&&(h.c1=c,h.c2=u,h.s1=i,h.s2=n,o.push(h))})}),o},makeshape:function(i,e,n){let r=e.points.length,s=i.points.length,o=le.makeline(e.points[r-1],i.points[0]),a=le.makeline(i.points[s-1],e.points[0]),l={startcap:o,forward:i,back:e,endcap:a,bbox:le.findbbox([o,i,e,a])};return l.intersections=function(c){return le.shapeintersections(l,l.bbox,c,c.bbox,n)},l},getminmax:function(i,e,n){if(!n)return{min:0,max:0};let r=qp,s=jp,o,a;n.indexOf(0)===-1&&(n=[0].concat(n)),n.indexOf(1)===-1&&n.push(1);for(let l=0,c=n.length;l<c;l++)o=n[l],a=i.get(o),a[e]<r&&(r=a[e]),a[e]>s&&(s=a[e]);return{min:r,mid:(r+s)/2,max:s,size:s-r}},align:function(i,e){let n=e.p1.x,r=e.p1.y,s=-qa(e.p2.y-r,e.p2.x-n),o=function(a){return{x:(a.x-n)*Vi(s)-(a.y-r)*co(s),y:(a.x-n)*co(s)+(a.y-r)*Vi(s)}};return i.map(o)},roots:function(i,e){e=e||{p1:{x:0,y:0},p2:{x:1,y:0}};let n=i.length-1,r=le.align(i,e),s=function(A){return 0<=A&&A<=1};if(n===2){let A=r[0].y,x=r[1].y,C=r[2].y,L=A-2*x+C;if(L!==0){let P=-Sr(x*x-A*C),O=-A+x,U=-(P+O)/L,M=-(-P+O)/L;return[U,M].filter(s)}else if(x!==C&&L===0)return[(2*x-C)/(2*x-2*C)].filter(s);return[]}let o=r[0].y,a=r[1].y,l=r[2].y,c=r[3].y,u=-o+3*a-3*l+c,h=3*o-6*a+3*l,d=-3*o+3*a,f=o;if(le.approximately(u,0)){if(le.approximately(h,0))return le.approximately(d,0)?[]:[-f/d].filter(s);let A=Sr(d*d-4*h*f),x=2*h;return[(A-d)/x,(-d-A)/x].filter(s)}h/=u,d/=u,f/=u;let p=(3*d-h*h)/3,g=p/3,v=(2*h*h*h-9*h*d+27*f)/27,_=v/2,m=_*_+g*g*g,w,T,y,b,S;if(m<0){let A=-p/3,x=A*A*A,C=Sr(x),L=-v/(2*C),P=L<-1?-1:L>1?1:L,O=EL(P),U=ja(C),M=2*U;return y=M*Vi(O/3)-h/3,b=M*Vi((O+Ph)/3)-h/3,S=M*Vi((O+2*Ph)/3)-h/3,[y,b,S].filter(s)}else{if(m===0)return w=_<0?ja(-_):-ja(_),y=2*w-h/3,b=-w-h/3,[y,b].filter(s);{let A=Sr(m);return w=ja(-_+A),T=ja(_+A),[w-T-h/3].filter(s)}}},droots:function(i){if(i.length===3){let e=i[0],n=i[1],r=i[2],s=e-2*n+r;if(s!==0){let o=-Sr(n*n-e*r),a=-e+n,l=-(o+a)/s,c=-(-o+a)/s;return[l,c]}else if(n!==r&&s===0)return[(2*n-r)/(2*(n-r))];return[]}if(i.length===2){let e=i[0],n=i[1];return e!==n?[e/(e-n)]:[]}return[]},curvature:function(i,e,n,r,s){let o,a,l,c,u=0,h=0,d=le.compute(i,e),f=le.compute(i,n),p=d.x*d.x+d.y*d.y;if(r?(o=Sr(zn(d.y*f.z-f.y*d.z,2)+zn(d.z*f.x-f.z*d.x,2)+zn(d.x*f.y-f.x*d.y,2)),a=zn(p+d.z*d.z,3/2)):(o=d.x*f.y-d.y*f.x,a=zn(p,3/2)),o===0||a===0)return{k:0,r:0};if(u=o/a,h=a/o,!s){let g=le.curvature(i-.001,e,n,r,!0).k,v=le.curvature(i+.001,e,n,r,!0).k;c=(v-u+(u-g))/2,l=(Xa(v-u)+Xa(u-g))/2}return{k:u,r:h,dk:c,adk:l}},inflections:function(i){if(i.length<4)return[];let e=le.align(i,{p1:i[0],p2:i.slice(-1)[0]}),n=e[2].x*e[1].y,r=e[3].x*e[1].y,s=e[1].x*e[2].y,o=e[3].x*e[2].y,a=18*(-3*n+2*r+3*s-o),l=18*(3*n-r-3*s),c=18*(s-n);if(le.approximately(a,0)){if(!le.approximately(l,0)){let f=-c/l;if(0<=f&&f<=1)return[f]}return[]}let u=2*a;if(le.approximately(u,0))return[];let h=l*l-4*a*c;if(h<0)return[];let d=Math.sqrt(h);return[(d-l)/u,-(l+d)/u].filter(function(f){return 0<=f&&f<=1})},bboxoverlap:function(i,e){let n=["x","y"],r=n.length;for(let s=0,o,a,l,c;s<r;s++)if(o=n[s],a=i[o].mid,l=e[o].mid,c=(i[o].size+e[o].size)/2,Xa(a-l)>=c)return!1;return!0},expandbox:function(i,e){e.x.min<i.x.min&&(i.x.min=e.x.min),e.y.min<i.y.min&&(i.y.min=e.y.min),e.z&&e.z.min<i.z.min&&(i.z.min=e.z.min),e.x.max>i.x.max&&(i.x.max=e.x.max),e.y.max>i.y.max&&(i.y.max=e.y.max),e.z&&e.z.max>i.z.max&&(i.z.max=e.z.max),i.x.mid=(i.x.min+i.x.max)/2,i.y.mid=(i.y.min+i.y.max)/2,i.z&&(i.z.mid=(i.z.min+i.z.max)/2),i.x.size=i.x.max-i.x.min,i.y.size=i.y.max-i.y.min,i.z&&(i.z.size=i.z.max-i.z.min)},pairiteration:function(i,e,n){let r=i.bbox(),s=e.bbox(),o=1e5,a=n||.5;if(r.x.size+r.y.size<a&&s.x.size+s.y.size<a)return[(o*(i._t1+i._t2)/2|0)/o+"/"+(o*(e._t1+e._t2)/2|0)/o];let l=i.split(.5),c=e.split(.5),u=[{left:l.left,right:c.left},{left:l.left,right:c.right},{left:l.right,right:c.right},{left:l.right,right:c.left}];u=u.filter(function(d){return le.bboxoverlap(d.left.bbox(),d.right.bbox())});let h=[];return u.length===0||(u.forEach(function(d){h=h.concat(le.pairiteration(d.left,d.right,a))}),h=h.filter(function(d,f){return h.indexOf(d)===f})),h},getccenter:function(i,e,n){let r=e.x-i.x,s=e.y-i.y,o=n.x-e.x,a=n.y-e.y,l=r*Vi(wr)-s*co(wr),c=r*co(wr)+s*Vi(wr),u=o*Vi(wr)-a*co(wr),h=o*co(wr)+a*Vi(wr),d=(i.x+e.x)/2,f=(i.y+e.y)/2,p=(e.x+n.x)/2,g=(e.y+n.y)/2,v=d+l,_=f+c,m=p+u,w=g+h,T=le.lli8(d,f,v,_,p,g,m,w),y=le.dist(T,i),b=qa(i.y-T.y,i.x-T.x),S=qa(e.y-T.y,e.x-T.x),A=qa(n.y-T.y,n.x-T.x),x;return b<A?((b>S||S>A)&&(b+=Ph),b>A&&(x=A,A=b,b=x)):A<S&&S<b?(x=A,A=b,b=x):A+=Ph,T.s=b,T.e=A,T.r=y,T},numberSort:function(i,e){return i-e}};var uo=class i{constructor(e){this.curves=[],this._3d=!1,e&&(this.curves=e,this._3d=this.curves[0]._3d)}valueOf(){return this.toString()}toString(){return"["+this.curves.map(function(e){return le.pointsToString(e.points)}).join(", ")+"]"}addCurve(e){this.curves.push(e),this._3d=this._3d||e._3d}length(){return this.curves.map(function(e){return e.length()}).reduce(function(e,n){return e+n})}curve(e){return this.curves[e]}bbox(){let e=this.curves;for(var n=e[0].bbox(),r=1;r<e.length;r++)le.expandbox(n,e[r].bbox());return n}offset(e){let n=[];return this.curves.forEach(function(r){n.push(...r.offset(e))}),new i(n)}};var{abs:Ya,min:bb,max:Sb,cos:CL,sin:RL,acos:PL,sqrt:$a}=Math,IL=Math.PI;var ns=class i{constructor(e){let n=e&&e.forEach?e:Array.from(arguments).slice(),r=!1;if(typeof n[0]=="object"){r=n.length;let p=[];n.forEach(function(g){["x","y","z"].forEach(function(v){typeof g[v]<"u"&&p.push(g[v])})}),n=p}let s=!1,o=n.length;if(r){if(r>4){if(arguments.length!==1)throw new Error("Only new Bezier(point[]) is accepted for 4th and higher order curves");s=!0}}else if(o!==6&&o!==8&&o!==9&&o!==12&&arguments.length!==1)throw new Error("Only new Bezier(point[]) is accepted for 4th and higher order curves");let a=this._3d=!s&&(o===9||o===12)||e&&e[0]&&typeof e[0].z<"u",l=this.points=[];for(let p=0,g=a?3:2;p<o;p+=g){var c={x:n[p],y:n[p+1]};a&&(c.z=n[p+2]),l.push(c)}let u=this.order=l.length-1,h=this.dims=["x","y"];a&&h.push("z"),this.dimlen=h.length;let d=le.align(l,{p1:l[0],p2:l[u]}),f=le.dist(l[0],l[u]);this._linear=d.reduce((p,g)=>p+Ya(g.y),0)<f/50,this._lut=[],this._t1=0,this._t2=1,this.update()}static quadraticFromPoints(e,n,r,s){if(typeof s>"u"&&(s=.5),s===0)return new i(n,n,r);if(s===1)return new i(e,n,n);let o=i.getABC(2,e,n,r,s);return new i(e,o.A,r)}static cubicFromPoints(e,n,r,s,o){typeof s>"u"&&(s=.5);let a=i.getABC(3,e,n,r,s);typeof o>"u"&&(o=le.dist(n,a.C));let l=o*(1-s)/s,c=le.dist(e,r),u=(r.x-e.x)/c,h=(r.y-e.y)/c,d=o*u,f=o*h,p=l*u,g=l*h,v={x:n.x-d,y:n.y-f},_={x:n.x+p,y:n.y+g},m=a.A,w={x:m.x+(v.x-m.x)/(1-s),y:m.y+(v.y-m.y)/(1-s)},T={x:m.x+(_.x-m.x)/s,y:m.y+(_.y-m.y)/s},y={x:e.x+(w.x-e.x)/s,y:e.y+(w.y-e.y)/s},b={x:r.x+(T.x-r.x)/(1-s),y:r.y+(T.y-r.y)/(1-s)};return new i(e,y,b,r)}static getUtils(){return le}getUtils(){return i.getUtils()}static get PolyBezier(){return uo}valueOf(){return this.toString()}toString(){return le.pointsToString(this.points)}toSVG(){if(this._3d)return!1;let e=this.points,n=e[0].x,r=e[0].y,s=["M",n,r,this.order===2?"Q":"C"];for(let o=1,a=e.length;o<a;o++)s.push(e[o].x),s.push(e[o].y);return s.join(" ")}setRatios(e){if(e.length!==this.points.length)throw new Error("incorrect number of ratio values");this.ratios=e,this._lut=[]}verify(){let e=this.coordDigest();e!==this._print&&(this._print=e,this.update())}coordDigest(){return this.points.map(function(e,n){return""+n+e.x+e.y+(e.z?e.z:0)}).join("")}update(){this._lut=[],this.dpoints=le.derive(this.points,this._3d),this.computedirection()}computedirection(){let e=this.points,n=le.angle(e[0],e[this.order],e[1]);this.clockwise=n>0}length(){return le.length(this.derivative.bind(this))}static getABC(e=2,n,r,s,o=.5){let a=le.projectionratio(o,e),l=1-a,c={x:a*n.x+l*s.x,y:a*n.y+l*s.y},u=le.abcratio(o,e);return{A:{x:r.x+(r.x-c.x)/u,y:r.y+(r.y-c.y)/u},B:r,C:c,S:n,E:s}}getABC(e,n){n=n||this.get(e);let r=this.points[0],s=this.points[this.order];return i.getABC(this.order,r,n,s,e)}getLUT(e){if(this.verify(),e=e||100,this._lut.length===e+1)return this._lut;this._lut=[],e++,this._lut=[];for(let n=0,r,s;n<e;n++)s=n/(e-1),r=this.compute(s),r.t=s,this._lut.push(r);return this._lut}on(e,n){n=n||5;let r=this.getLUT(),s=[];for(let o=0,a,l=0;o<r.length;o++)a=r[o],le.dist(a,e)<n&&(s.push(a),l+=o/r.length);return s.length?t/=s.length:!1}project(e){let n=this.getLUT(),r=n.length-1,s=le.closest(n,e),o=s.mpos,a=(o-1)/r,l=(o+1)/r,c=.1/r,u=s.mdist,h=a,d=h,f;u+=1;for(let p;h<l+c;h+=c)f=this.compute(h),p=le.dist(e,f),p<u&&(u=p,d=h);return d=d<0?0:d>1?1:d,f=this.compute(d),f.t=d,f.d=u,f}get(e){return this.compute(e)}point(e){return this.points[e]}compute(e){return this.ratios?le.computeWithRatios(e,this.points,this.ratios,this._3d):le.compute(e,this.points,this._3d,this.ratios)}raise(){let e=this.points,n=[e[0]],r=e.length;for(let s=1,o,a;s<r;s++)o=e[s],a=e[s-1],n[s]={x:(r-s)/r*o.x+s/r*a.x,y:(r-s)/r*o.y+s/r*a.y};return n[r]=e[r-1],new i(n)}derivative(e){return le.compute(e,this.dpoints[0],this._3d)}dderivative(e){return le.compute(e,this.dpoints[1],this._3d)}align(){let e=this.points;return new i(le.align(e,{p1:e[0],p2:e[e.length-1]}))}curvature(e){return le.curvature(e,this.dpoints[0],this.dpoints[1],this._3d)}inflections(){return le.inflections(this.points)}normal(e){return this._3d?this.__normal3(e):this.__normal2(e)}__normal2(e){let n=this.derivative(e),r=$a(n.x*n.x+n.y*n.y);return{t:e,x:-n.y/r,y:n.x/r}}__normal3(e){let n=this.derivative(e),r=this.derivative(e+.01),s=$a(n.x*n.x+n.y*n.y+n.z*n.z),o=$a(r.x*r.x+r.y*r.y+r.z*r.z);n.x/=s,n.y/=s,n.z/=s,r.x/=o,r.y/=o,r.z/=o;let a={x:r.y*n.z-r.z*n.y,y:r.z*n.x-r.x*n.z,z:r.x*n.y-r.y*n.x},l=$a(a.x*a.x+a.y*a.y+a.z*a.z);a.x/=l,a.y/=l,a.z/=l;let c=[a.x*a.x,a.x*a.y-a.z,a.x*a.z+a.y,a.x*a.y+a.z,a.y*a.y,a.y*a.z-a.x,a.x*a.z-a.y,a.y*a.z+a.x,a.z*a.z];return{t:e,x:c[0]*n.x+c[1]*n.y+c[2]*n.z,y:c[3]*n.x+c[4]*n.y+c[5]*n.z,z:c[6]*n.x+c[7]*n.y+c[8]*n.z}}hull(e){let n=this.points,r=[],s=[],o=0;for(s[o++]=n[0],s[o++]=n[1],s[o++]=n[2],this.order===3&&(s[o++]=n[3]);n.length>1;){r=[];for(let a=0,l,c=n.length-1;a<c;a++)l=le.lerp(e,n[a],n[a+1]),s[o++]=l,r.push(l);n=r}return s}split(e,n){if(e===0&&n)return this.split(n).left;if(n===1)return this.split(e).right;let r=this.hull(e),s={left:this.order===2?new i([r[0],r[3],r[5]]):new i([r[0],r[4],r[7],r[9]]),right:this.order===2?new i([r[5],r[4],r[2]]):new i([r[9],r[8],r[6],r[3]]),span:r};return s.left._t1=le.map(0,0,1,this._t1,this._t2),s.left._t2=le.map(e,0,1,this._t1,this._t2),s.right._t1=le.map(e,0,1,this._t1,this._t2),s.right._t2=le.map(1,0,1,this._t1,this._t2),n?(n=le.map(n,e,1,0,1),s.right.split(n).left):s}extrema(){let e={},n=[];return this.dims.forEach(function(r){let s=function(a){return a[r]},o=this.dpoints[0].map(s);e[r]=le.droots(o),this.order===3&&(o=this.dpoints[1].map(s),e[r]=e[r].concat(le.droots(o))),e[r]=e[r].filter(function(a){return a>=0&&a<=1}),n=n.concat(e[r].sort(le.numberSort))}.bind(this)),e.values=n.sort(le.numberSort).filter(function(r,s){return n.indexOf(r)===s}),e}bbox(){let e=this.extrema(),n={};return this.dims.forEach(function(r){n[r]=le.getminmax(this,r,e[r])}.bind(this)),n}overlaps(e){let n=this.bbox(),r=e.bbox();return le.bboxoverlap(n,r)}offset(e,n){if(typeof n<"u"){let r=this.get(e),s=this.normal(e),o={c:r,n:s,x:r.x+s.x*n,y:r.y+s.y*n};return this._3d&&(o.z=r.z+s.z*n),o}if(this._linear){let r=this.normal(0),s=this.points.map(function(o){let a={x:o.x+e*r.x,y:o.y+e*r.y};return o.z&&r.z&&(a.z=o.z+e*r.z),a});return[new i(s)]}return this.reduce().map(function(r){return r._linear?r.offset(e)[0]:r.scale(e)})}simple(){if(this.order===3){let s=le.angle(this.points[0],this.points[3],this.points[1]),o=le.angle(this.points[0],this.points[3],this.points[2]);if(s>0&&o<0||s<0&&o>0)return!1}let e=this.normal(0),n=this.normal(1),r=e.x*n.x+e.y*n.y;return this._3d&&(r+=e.z*n.z),Ya(PL(r))<IL/3}reduce(){let e,n=0,r=0,s=.01,o,a=[],l=[],c=this.extrema().values;for(c.indexOf(0)===-1&&(c=[0].concat(c)),c.indexOf(1)===-1&&c.push(1),n=c[0],e=1;e<c.length;e++)r=c[e],o=this.split(n,r),o._t1=n,o._t2=r,a.push(o),n=r;return a.forEach(function(u){for(n=0,r=0;r<=1;)for(r=n+s;r<=1+s;r+=s)if(o=u.split(n,r),!o.simple()){if(r-=s,Ya(n-r)<s)return[];o=u.split(n,r),o._t1=le.map(n,0,1,u._t1,u._t2),o._t2=le.map(r,0,1,u._t1,u._t2),l.push(o),n=r;break}n<1&&(o=u.split(n,1),o._t1=le.map(n,0,1,u._t1,u._t2),o._t2=u._t2,l.push(o))}),l}translate(e,n,r){r=typeof r=="number"?r:n;let s=this.order,o=this.points.map((a,l)=>(1-l/s)*n+l/s*r);return new i(this.points.map((a,l)=>({x:a.x+e.x*o[l],y:a.y+e.y*o[l]})))}scale(e){let n=this.order,r=!1;if(typeof e=="function"&&(r=e),r&&n===2)return this.raise().scale(r);let s=this.clockwise,o=this.points;if(this._linear)return this.translate(this.normal(0),r?r(0):e,r?r(1):e);let a=r?r(0):e,l=r?r(1):e,c=[this.offset(0,10),this.offset(1,10)],u=[],h=le.lli4(c[0],c[0].c,c[1],c[1].c);if(!h)throw new Error("cannot scale this curve. Try reducing it first.");return[0,1].forEach(function(d){let f=u[d*n]=le.copy(o[d*n]);f.x+=(d?l:a)*c[d].n.x,f.y+=(d?l:a)*c[d].n.y}),r?([0,1].forEach(function(d){if(!(n===2&&d)){var f=o[d+1],p={x:f.x-h.x,y:f.y-h.y},g=r?r((d+1)/n):e;r&&!s&&(g=-g);var v=$a(p.x*p.x+p.y*p.y);p.x/=v,p.y/=v,u[d+1]={x:f.x+g*p.x,y:f.y+g*p.y}}}),new i(u)):([0,1].forEach(d=>{if(n===2&&d)return;let f=u[d*n],p=this.derivative(d),g={x:f.x+p.x,y:f.y+p.y};u[d+1]=le.lli4(f,g,h,o[d+1])}),new i(u))}outline(e,n,r,s){if(n=n===void 0?e:n,this._linear){let b=this.normal(0),S=this.points[0],A=this.points[this.points.length-1],x,C,L;r===void 0&&(r=e,s=n),x={x:S.x+b.x*e,y:S.y+b.y*e},L={x:A.x+b.x*r,y:A.y+b.y*r},C={x:(x.x+L.x)/2,y:(x.y+L.y)/2};let P=[x,C,L];x={x:S.x-b.x*n,y:S.y-b.y*n},L={x:A.x-b.x*s,y:A.y-b.y*s},C={x:(x.x+L.x)/2,y:(x.y+L.y)/2};let O=[L,C,x],U=le.makeline(O[2],P[0]),M=le.makeline(P[2],O[0]),I=[U,new i(P),M,new i(O)];return new uo(I)}let o=this.reduce(),a=o.length,l=[],c=[],u,h=0,d=this.length(),f=typeof r<"u"&&typeof s<"u";function p(b,S,A,x,C){return function(L){let P=x/A,O=(x+C)/A,U=S-b;return le.map(L,0,1,b+P*U,b+O*U)}}o.forEach(function(b){let S=b.length();f?(l.push(b.scale(p(e,r,d,h,S))),c.push(b.scale(p(-n,-s,d,h,S)))):(l.push(b.scale(e)),c.push(b.scale(-n))),h+=S}),c=c.map(function(b){return u=b.points,u[3]?b.points=[u[3],u[2],u[1],u[0]]:b.points=[u[2],u[1],u[0]],b}).reverse();let g=l[0].points[0],v=l[a-1].points[l[a-1].points.length-1],_=c[a-1].points[c[a-1].points.length-1],m=c[0].points[0],w=le.makeline(_,g),T=le.makeline(v,m),y=[w].concat(l).concat([T]).concat(c);return new uo(y)}outlineshapes(e,n,r){n=n||e;let s=this.outline(e,n).curves,o=[];for(let a=1,l=s.length;a<l/2;a++){let c=le.makeshape(s[a],s[l-a],r);c.startcap.virtual=a>1,c.endcap.virtual=a<l/2-1,o.push(c)}return o}intersects(e,n){return e?e.p1&&e.p2?this.lineIntersects(e):(e instanceof i&&(e=e.reduce()),this.curveintersects(this.reduce(),e,n)):this.selfintersects(n)}lineIntersects(e){let n=bb(e.p1.x,e.p2.x),r=bb(e.p1.y,e.p2.y),s=Sb(e.p1.x,e.p2.x),o=Sb(e.p1.y,e.p2.y);return le.roots(this.points,e).filter(a=>{var l=this.get(a);return le.between(l.x,n,s)&&le.between(l.y,r,o)})}selfintersects(e){let n=this.reduce(),r=n.length-2,s=[];for(let o=0,a,l,c;o<r;o++)l=n.slice(o,o+1),c=n.slice(o+2),a=this.curveintersects(l,c,e),s.push(...a);return s}curveintersects(e,n,r){let s=[];e.forEach(function(a){n.forEach(function(l){a.overlaps(l)&&s.push({left:a,right:l})})});let o=[];return s.forEach(function(a){let l=le.pairiteration(a.left,a.right,r);l.length>0&&(o=o.concat(l))}),o}arcs(e){return e=e||.5,this._iterate(e,[])}_error(e,n,r,s){let o=(s-r)/4,a=this.get(r+o),l=this.get(s-o),c=le.dist(e,n),u=le.dist(e,a),h=le.dist(e,l);return Ya(u-c)+Ya(h-c)}_iterate(e,n){let r=0,s=1,o;do{o=0,s=1;let a=this.get(r),l,c,u,h,d=!1,f=!1,p,g=s,v=1,_=0;do if(f=d,h=u,g=(r+s)/2,_++,l=this.get(g),c=this.get(s),u=le.getccenter(a,l,c),u.interval={start:r,end:s},d=this._error(u,a,r,s)<=e,p=f&&!d,p||(v=s),d){if(s>=1){if(u.interval.end=v=1,h=u,s>1){let w={x:u.x+u.r*CL(u.e),y:u.y+u.r*RL(u.e)};u.e+=le.angle({x:u.x,y:u.y},w,this.get(1))}break}s=s+(s-r)/2}else s=g;while(!p&&o++<100);if(o>=100)break;h=h||u,n.push(h),r=v}while(s<1);return n}};function Yp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function LL(i){if(Array.isArray(i))return i}function DL(i){if(Array.isArray(i))return Yp(i)}function OL(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function NL(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function UL(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function FL(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function kL(i,e){if(i==null)return{};var n,r,s=BL(i,e);if(Object.getOwnPropertySymbols){var o=Object.getOwnPropertySymbols(i);for(r=0;r<o.length;r++)n=o[r],e.includes(n)||{}.propertyIsEnumerable.call(i,n)&&(s[n]=i[n])}return s}function BL(i,e){if(i==null)return{};var n={};for(var r in i)if({}.hasOwnProperty.call(i,r)){if(e.includes(r))continue;n[r]=i[r]}return n}function zL(i,e){return LL(i)||NL(i,e)||wb(i,e)||UL()}function VL(i){return DL(i)||OL(i)||wb(i)||FL()}function GL(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return String(i)}function HL(i){var e=GL(i,"string");return typeof e=="symbol"?e:e+""}function wb(i,e){if(i){if(typeof i=="string")return Yp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Yp(i,e):void 0}}var Mb=(function(){var i=arguments.length>0&&arguments[0]!==void 0?arguments[0]:[],e=arguments.length>1&&arguments[1]!==void 0?arguments[1]:[],n=arguments.length>2&&arguments[2]!==void 0?arguments[2]:!0,r=arguments.length>3&&arguments[3]!==void 0?arguments[3]:!1,s=(e instanceof Array?e.length?e:[void 0]:[e]).map(function(l){return{keyAccessor:l,isProp:!(l instanceof Function)}}),o=i.reduce(function(l,c){var u=l,h=c;return s.forEach(function(d,f){var p=d.keyAccessor,g=d.isProp,v;if(g){var _=h,m=_[p],w=kL(_,[p].map(HL));v=m,h=w}else v=p(h,f);f+1<s.length?(u.hasOwnProperty(v)||(u[v]={}),u=u[v]):n?(u.hasOwnProperty(v)||(u[v]=[]),u[v].push(h)):u[v]=h}),l},{});n instanceof Function&&(function l(c){var u=arguments.length>1&&arguments[1]!==void 0?arguments[1]:1;u===s.length?Object.keys(c).forEach(function(h){return c[h]=n(c[h])}):Object.values(c).forEach(function(h){return l(h,u+1)})})(o);var a=o;return r&&(a=[],(function l(c){var u=arguments.length>1&&arguments[1]!==void 0?arguments[1]:[];u.length===s.length?a.push({keys:u,vals:c}):Object.entries(c).forEach(function(h){var d=zL(h,2),f=d[0],p=d[1];return l(p,[].concat(VL(u),[f]))})})(o),e instanceof Array&&e.length===0&&a.length===1&&(a[0].keys=[])),a});function WL(i,e){e===void 0&&(e={});var n=e.insertAt;if(!(typeof document>"u")){var r=document.head||document.getElementsByTagName("head")[0],s=document.createElement("style");s.type="text/css",n==="top"&&r.firstChild?r.insertBefore(s,r.firstChild):r.appendChild(s),s.styleSheet?s.styleSheet.cssText=i:s.appendChild(document.createTextNode(i))}}var XL=`.force-graph-container canvas {
  display: block;
  user-select: none;
  outline: none;
  -webkit-tap-highlight-color: transparent;
}

.force-graph-container .clickable {
  cursor: pointer;
}

.force-graph-container .grabbable {
  cursor: move;
  cursor: grab;
  cursor: -moz-grab;
  cursor: -webkit-grab;
}

.force-graph-container .grabbable:active {
  cursor: grabbing;
  cursor: -moz-grabbing;
  cursor: -webkit-grabbing;
}
`;WL(XL);function Zp(i,e){(e==null||e>i.length)&&(e=i.length);for(var n=0,r=Array(e);n<e;n++)r[n]=i[n];return r}function qL(i){if(Array.isArray(i))return i}function jL(i){if(Array.isArray(i))return Zp(i)}function Eb(i,e,n){if(Pb())return Reflect.construct.apply(null,arguments);var r=[null];r.push.apply(r,e);var s=new(i.bind.apply(i,r));return s}function Ka(i,e,n){return(e=QL(e))in i?Object.defineProperty(i,e,{value:n,enumerable:!0,configurable:!0,writable:!0}):i[e]=n,i}function Pb(){try{var i=!Boolean.prototype.valueOf.call(Reflect.construct(Boolean,[],function(){}))}catch{}return(Pb=function(){return!!i})()}function YL(i){if(typeof Symbol<"u"&&i[Symbol.iterator]!=null||i["@@iterator"]!=null)return Array.from(i)}function $L(i,e){var n=i==null?null:typeof Symbol<"u"&&i[Symbol.iterator]||i["@@iterator"];if(n!=null){var r,s,o,a,l=[],c=!0,u=!1;try{if(o=(n=n.call(i)).next,e!==0)for(;!(c=(r=o.call(n)).done)&&(l.push(r.value),l.length!==e);c=!0);}catch(h){u=!0,s=h}finally{try{if(!c&&n.return!=null&&(a=n.return(),Object(a)!==a))return}finally{if(u)throw s}}return l}}function ZL(){throw new TypeError(`Invalid attempt to destructure non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function KL(){throw new TypeError(`Invalid attempt to spread non-iterable instance.
In order to be iterable, non-array objects must have a [Symbol.iterator]() method.`)}function Ab(i,e){var n=Object.keys(i);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(i);e&&(r=r.filter(function(s){return Object.getOwnPropertyDescriptor(i,s).enumerable})),n.push.apply(n,r)}return n}function ho(i){for(var e=1;e<arguments.length;e++){var n=arguments[e]!=null?arguments[e]:{};e%2?Ab(Object(n),!0).forEach(function(r){Ka(i,r,n[r])}):Object.getOwnPropertyDescriptors?Object.defineProperties(i,Object.getOwnPropertyDescriptors(n)):Ab(Object(n)).forEach(function(r){Object.defineProperty(i,r,Object.getOwnPropertyDescriptor(n,r))})}return i}function Za(i,e){return qL(i)||$L(i,e)||Ib(i,e)||ZL()}function Vn(i){return jL(i)||YL(i)||Ib(i)||KL()}function JL(i,e){if(typeof i!="object"||!i)return i;var n=i[Symbol.toPrimitive];if(n!==void 0){var r=n.call(i,e);if(typeof r!="object")return r;throw new TypeError("@@toPrimitive must return a primitive value.")}return(e==="string"?String:Number)(i)}function QL(i){var e=JL(i,"string");return typeof e=="symbol"?e:e+""}function Kp(i){"@babel/helpers - typeof";return Kp=typeof Symbol=="function"&&typeof Symbol.iterator=="symbol"?function(e){return typeof e}:function(e){return e&&typeof Symbol=="function"&&e.constructor===Symbol&&e!==Symbol.prototype?"symbol":typeof e},Kp(i)}function Ib(i,e){if(i){if(typeof i=="string")return Zp(i,e);var n={}.toString.call(i).slice(8,-1);return n==="Object"&&i.constructor&&(n=i.constructor.name),n==="Map"||n==="Set"?Array.from(i):n==="Arguments"||/^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)?Zp(i,e):void 0}}var eD=qr(ba);function Tb(i,e,n){!e||typeof n!="string"||i.filter(function(r){return!r[n]}).forEach(function(r){r[n]=eD(e(r))})}function tD(i,e){var n=i.nodes,r=i.links,s=arguments.length>2&&arguments[2]!==void 0?arguments[2]:{},o=s.nodeFilter,a=o===void 0?function(){return!0}:o,l=s.onLoopError,c=l===void 0?function(p){throw"Invalid DAG structure! Found cycle in node path: ".concat(p.join(" -> "),".")}:l,u={};n.forEach(function(p){return u[e(p)]={data:p,out:[],depth:-1,skip:!a(p)}}),r.forEach(function(p){var g=p.source,v=p.target,_=y(g),m=y(v);if(!u.hasOwnProperty(_))throw"Missing source node with id: ".concat(_);if(!u.hasOwnProperty(m))throw"Missing target node with id: ".concat(m);var w=u[_],T=u[m];w.out.push(T);function y(b){return Kp(b)==="object"?e(b):b}});var h=[];f(Object.values(u));var d=Object.assign.apply(Object,[{}].concat(Vn(Object.entries(u).filter(function(p){var g=Za(p,2),v=g[1];return!v.skip}).map(function(p){var g=Za(p,2),v=g[0],_=g[1];return Ka({},v,_.depth)}))));return d;function f(p){for(var g=arguments.length>1&&arguments[1]!==void 0?arguments[1]:[],v=arguments.length>2&&arguments[2]!==void 0?arguments[2]:0,_=function(){var y=p[m];if(g.indexOf(y)!==-1){var b=[].concat(Vn(g.slice(g.indexOf(y))),[y]).map(function(S){return e(S.data)});return h.some(function(S){return S.length===b.length&&S.every(function(A,x){return A===b[x]})})||(h.push(b),c(b)),1}v>y.depth&&(y.depth=v,f(y.out,[].concat(Vn(g),[y]),v+(y.skip?0:1)))},m=0,w=p.length;m<w;m++)_()}}var nD=2,nn=function(e,n){return n.onNeedsRedraw&&n.onNeedsRedraw()},Cb=function(e,n){if(!n.isShadow){var r=we(n.linkDirectionalParticles);n.graphData.links.forEach(function(s){var o=Math.round(Math.abs(r(s)));o?s.__photons=Vn(Array(o)).map(function(){return{}}):delete s.__photons})}},Ih=ei({props:{graphData:{default:{nodes:[],links:[]},onChange:function(e,n){n.engineRunning=!1,Cb(e,n)}},dagMode:{onChange:function(e,n){!e&&(n.graphData.nodes||[]).forEach(function(r){return r.fx=r.fy=void 0})}},dagLevelDistance:{},dagNodeFilter:{default:function(e){return!0}},onDagError:{triggerUpdate:!1},nodeRelSize:{default:4,triggerUpdate:!1,onChange:nn},nodeId:{default:"id"},nodeVal:{default:"val",triggerUpdate:!1,onChange:nn},nodeColor:{default:"color",triggerUpdate:!1,onChange:nn},nodeAutoColorBy:{},nodeCanvasObject:{triggerUpdate:!1,onChange:nn},nodeCanvasObjectMode:{default:function(){return"replace"},triggerUpdate:!1,onChange:nn},nodeVisibility:{default:!0,triggerUpdate:!1,onChange:nn},linkSource:{default:"source"},linkTarget:{default:"target"},linkVisibility:{default:!0,triggerUpdate:!1,onChange:nn},linkColor:{default:"color",triggerUpdate:!1,onChange:nn},linkAutoColorBy:{},linkLineDash:{triggerUpdate:!1,onChange:nn},linkWidth:{default:1,triggerUpdate:!1,onChange:nn},linkCurvature:{default:0,triggerUpdate:!1,onChange:nn},linkCanvasObject:{triggerUpdate:!1,onChange:nn},linkCanvasObjectMode:{default:function(){return"replace"},triggerUpdate:!1,onChange:nn},linkDirectionalArrowLength:{default:0,triggerUpdate:!1,onChange:nn},linkDirectionalArrowColor:{triggerUpdate:!1,onChange:nn},linkDirectionalArrowRelPos:{default:.5,triggerUpdate:!1,onChange:nn},linkDirectionalParticles:{default:0,triggerUpdate:!1,onChange:Cb},linkDirectionalParticleSpeed:{default:.01,triggerUpdate:!1},linkDirectionalParticleOffset:{default:0,triggerUpdate:!1},linkDirectionalParticleWidth:{default:4,triggerUpdate:!1},linkDirectionalParticleColor:{triggerUpdate:!1},linkDirectionalParticleCanvasObject:{triggerUpdate:!1},globalScale:{default:1,triggerUpdate:!1},d3AlphaMin:{default:0,triggerUpdate:!1},d3AlphaDecay:{default:.0228,triggerUpdate:!1,onChange:function(e,n){n.forceLayout.alphaDecay(e)}},d3AlphaTarget:{default:0,triggerUpdate:!1,onChange:function(e,n){n.forceLayout.alphaTarget(e)}},d3VelocityDecay:{default:.4,triggerUpdate:!1,onChange:function(e,n){n.forceLayout.velocityDecay(e)}},warmupTicks:{default:0,triggerUpdate:!1},cooldownTicks:{default:1/0,triggerUpdate:!1},cooldownTime:{default:15e3,triggerUpdate:!1},onUpdate:{default:function(){},triggerUpdate:!1},onFinishUpdate:{default:function(){},triggerUpdate:!1},onEngineTick:{default:function(){},triggerUpdate:!1},onEngineStop:{default:function(){},triggerUpdate:!1},onNeedsRedraw:{triggerUpdate:!1},isShadow:{default:!1,triggerUpdate:!1}},methods:{d3Force:function(e,n,r){return r===void 0?e.forceLayout.force(n):(e.forceLayout.force(n,r),this)},d3ReheatSimulation:function(e){return e.forceLayout.alpha(1),this.resetCountdown(),this},resetCountdown:function(e){return e.cntTicks=0,e.startTickTime=new Date,e.engineRunning=!0,this},isEngineRunning:function(e){return!!e.engineRunning},tickFrame:function(e){return!e.isShadow&&n(),s(),!e.isShadow&&o(),!e.isShadow&&a(),r(),this;function n(){e.engineRunning&&(++e.cntTicks>e.cooldownTicks||new Date-e.startTickTime>e.cooldownTime||e.d3AlphaMin>0&&e.forceLayout.alpha()<e.d3AlphaMin?(e.engineRunning=!1,e.onEngineStop()):(e.forceLayout.tick(),e.onEngineTick()))}function r(){var l=we(e.nodeVisibility),c=we(e.nodeVal),u=we(e.nodeColor),h=we(e.nodeCanvasObjectMode),d=e.ctx,f=e.isShadow/e.globalScale,p=e.graphData.nodes.filter(l);d.save(),p.forEach(function(g){var v=h(g);if(e.nodeCanvasObject&&(v==="before"||v==="replace")&&(e.nodeCanvasObject(g,d,e.globalScale),v==="replace")){d.restore();return}var _=Math.sqrt(Math.max(0,c(g)||1))*e.nodeRelSize+f;d.beginPath(),d.arc(g.x,g.y,_,0,2*Math.PI,!1),d.fillStyle=u(g)||"rgba(31, 120, 180, 0.92)",d.fill(),e.nodeCanvasObject&&v==="after"&&e.nodeCanvasObject(g,e.ctx,e.globalScale)}),d.restore()}function s(){var l=we(e.linkVisibility),c=we(e.linkColor),u=we(e.linkWidth),h=we(e.linkLineDash),d=we(e.linkCurvature),f=we(e.linkCanvasObjectMode),p=e.ctx,g=e.isShadow*2,v=e.graphData.links.filter(l);v.forEach(S);var _=[],m=[],w=v;if(e.linkCanvasObject){var T=[],y=[];v.forEach(function(A){return({before:_,after:m,replace:T}[f(A)]||y).push(A)}),w=[].concat(Vn(_),m,y),_=_.concat(T)}p.save(),_.forEach(function(A){return e.linkCanvasObject(A,p,e.globalScale)}),p.restore();var b=Mb(w,[c,u,h]);p.save(),Object.entries(b).forEach(function(A){var x=Za(A,2),C=x[0],L=x[1],P=!C||C==="undefined"?"rgba(0,0,0,0.15)":C;Object.entries(L).forEach(function(O){var U=Za(O,2),M=U[0],I=U[1],D=(M||1)/e.globalScale+g;Object.entries(I).forEach(function(k){var q=Za(k,2);q[0];var W=q[1],j=h(W[0]);p.beginPath(),W.forEach(function(B){var Q=B.source,re=B.target;if(!(!Q||!re||!Q.hasOwnProperty("x")||!re.hasOwnProperty("x"))){p.moveTo(Q.x,Q.y);var be=B.__controlPoints;be?p[be.length===2?"quadraticCurveTo":"bezierCurveTo"].apply(p,Vn(be).concat([re.x,re.y])):p.lineTo(re.x,re.y)}}),p.strokeStyle=P,p.lineWidth=D,p.setLineDash(j||[]),p.stroke()})})}),p.restore(),p.save(),m.forEach(function(A){return e.linkCanvasObject(A,p,e.globalScale)}),p.restore();function S(A){var x=d(A);if(!x){A.__controlPoints=null;return}var C=A.source,L=A.target;if(!(!C||!L||!C.hasOwnProperty("x")||!L.hasOwnProperty("x"))){var P=Math.sqrt(Math.pow(L.x-C.x,2)+Math.pow(L.y-C.y,2));if(P>0){var O=Math.atan2(L.y-C.y,L.x-C.x),U=P*x,M={x:(C.x+L.x)/2+U*Math.cos(O-Math.PI/2),y:(C.y+L.y)/2+U*Math.sin(O-Math.PI/2)};A.__controlPoints=[M.x,M.y]}else{var I=x*70;A.__controlPoints=[L.x,L.y-I,L.x+I,L.y]}}}}function o(){var l=1.6,c=.2,u=we(e.linkDirectionalArrowLength),h=we(e.linkDirectionalArrowRelPos),d=we(e.linkVisibility),f=we(e.linkDirectionalArrowColor||e.linkColor),p=we(e.nodeVal),g=e.ctx;g.save(),e.graphData.links.filter(d).forEach(function(v){var _=u(v);if(!(!_||_<0)){var m=v.source,w=v.target;if(!(!m||!w||!m.hasOwnProperty("x")||!w.hasOwnProperty("x"))){var T=Math.sqrt(Math.max(0,p(m)||1))*e.nodeRelSize,y=Math.sqrt(Math.max(0,p(w)||1))*e.nodeRelSize,b=Math.min(1,Math.max(0,h(v))),S=f(v)||"rgba(0,0,0,0.28)",A=_/l/2,x=v.__controlPoints&&Eb(ns,[m.x,m.y].concat(Vn(v.__controlPoints),[w.x,w.y])),C=x?function(D){return x.get(D)}:function(D){return{x:m.x+(w.x-m.x)*D||0,y:m.y+(w.y-m.y)*D||0}},L=x?x.length():Math.sqrt(Math.pow(w.x-m.x,2)+Math.pow(w.y-m.y,2)),P=T+_+(L-T-y-_)*b,O=C(P/L),U=C((P-_)/L),M=C((P-_*(1-c))/L),I=Math.atan2(O.y-U.y,O.x-U.x)-Math.PI/2;g.beginPath(),g.moveTo(O.x,O.y),g.lineTo(U.x+A*Math.cos(I),U.y+A*Math.sin(I)),g.lineTo(M.x,M.y),g.lineTo(U.x-A*Math.cos(I),U.y-A*Math.sin(I)),g.fillStyle=S,g.fill()}}}),g.restore()}function a(){var l=we(e.linkDirectionalParticles),c=we(e.linkDirectionalParticleSpeed),u=we(e.linkDirectionalParticleOffset),h=we(e.linkDirectionalParticleWidth),d=we(e.linkVisibility),f=we(e.linkDirectionalParticleColor||e.linkColor),p=e.ctx;p.save(),e.graphData.links.filter(d).forEach(function(g){var v=l(g);if(!(!g.hasOwnProperty("__photons")||!g.__photons.length)){var _=g.source,m=g.target;if(!(!_||!m||!_.hasOwnProperty("x")||!m.hasOwnProperty("x"))){var w=c(g),T=Math.abs(u(g)),y=g.__photons||[],b=Math.max(0,h(g)/2)/Math.sqrt(e.globalScale),S=f(g)||"rgba(0,0,0,0.28)";p.fillStyle=S;var A=g.__controlPoints?Eb(ns,[_.x,_.y].concat(Vn(g.__controlPoints),[m.x,m.y])):null,x=0,C=!1;y.forEach(function(L){var P=!!L.__singleHop;if(L.hasOwnProperty("__progressRatio")||(L.__progressRatio=P?w<0?1:0:(x+T)/v),!P&&x++,L.__progressRatio+=w,L.__progressRatio>=1||L.__progressRatio<0)if(!P)L.__progressRatio=L.__progressRatio%1,L.__progressRatio<0&&L.__progressRatio++;else{C=!0;return}var O=L.__progressRatio,U=A?A.get(O):{x:_.x+(m.x-_.x)*O||0,y:_.y+(m.y-_.y)*O||0};e.linkDirectionalParticleCanvasObject?e.linkDirectionalParticleCanvasObject(U.x,U.y,g,p,e.globalScale):(p.beginPath(),p.arc(U.x,U.y,b,0,2*Math.PI,!1),p.fill())}),C&&(g.__photons=g.__photons.filter(function(L){return!L.__singleHop||L.__progressRatio<=1&&L.__progressRatio>=0}))}}}),p.restore()}},emitParticle:function(e,n){return n&&(!n.__photons&&(n.__photons=[]),n.__photons.push({__singleHop:!0})),this}},stateInit:function(){return{forceLayout:fa().force("link",sa()).force("charge",da()).force("center",ta()).force("dagRadial",null).stop(),engineRunning:!1}},init:function(e,n){n.ctx=e},update:function(e,n){e.engineRunning=!1,e.onUpdate(),e.nodeAutoColorBy!==null&&Tb(e.graphData.nodes,we(e.nodeAutoColorBy),e.nodeColor),e.linkAutoColorBy!==null&&Tb(e.graphData.links,we(e.linkAutoColorBy),e.linkColor),e.graphData.links.forEach(function(f){f.source=f[e.linkSource],f.target=f[e.linkTarget]}),e.forceLayout.stop().alpha(1).nodes(e.graphData.nodes);var r=e.forceLayout.force("link");r&&r.id(function(f){return f[e.nodeId]}).links(e.graphData.links);var s=e.dagMode&&tD(e.graphData,function(f){return f[e.nodeId]},{nodeFilter:e.dagNodeFilter,onLoopError:e.onDagError||void 0}),o=Math.max.apply(Math,Vn(Object.values(s||[]))),a=e.dagLevelDistance||e.graphData.nodes.length/(o||1)*nD*(["radialin","radialout"].indexOf(e.dagMode)!==-1?.7:1);if(["lr","rl","td","bu"].includes(n.dagMode)){var l=["lr","rl"].includes(n.dagMode)?"fx":"fy";e.graphData.nodes.filter(e.dagNodeFilter).forEach(function(f){return delete f[l]})}if(["lr","rl","td","bu"].includes(e.dagMode)){var c=["rl","bu"].includes(e.dagMode),u=function(p){return(s[p[e.nodeId]]-o/2)*a*(c?-1:1)},h=["lr","rl"].includes(e.dagMode)?"fx":"fy";e.graphData.nodes.filter(e.dagNodeFilter).forEach(function(f){return f[h]=u(f)})}e.forceLayout.force("dagRadial",["radialin","radialout"].indexOf(e.dagMode)!==-1?pa(function(f){var p=s[f[e.nodeId]]||-1;return(e.dagMode==="radialin"?o-p:p)*a}).strength(function(f){return e.dagNodeFilter(f)?1:0}):null);for(var d=0;d<e.warmupTicks&&!(e.d3AlphaMin>0&&e.forceLayout.alpha()<e.d3AlphaMin);d++)e.forceLayout.tick();this.resetCountdown(),e.onFinishUpdate()}});function Lb(i,e){var n=i instanceof Array?i:[i],r=new e;return r._destructor&&r._destructor(),{linkProp:function(o){return{default:r[o](),onChange:function(l,c){n.forEach(function(u){return c[u][o](l)})},triggerUpdate:!1}},linkMethod:function(o){return function(a){for(var l=arguments.length,c=new Array(l>1?l-1:0),u=1;u<l;u++)c[u-1]=arguments[u];var h=[];return n.forEach(function(d){var f=a[d],p=f[o].apply(f,c);p!==f&&h.push(p)}),h.length?h[0]:this}}}}var iD=800,rD=4,sD=5,Db=Lb("forceGraph",Ih),oD=Lb(["forceGraph","shadowGraph"],Ih),aD=Object.assign.apply(Object,Vn(["nodeColor","nodeAutoColorBy","nodeCanvasObject","nodeCanvasObjectMode","linkColor","linkAutoColorBy","linkLineDash","linkWidth","linkCanvasObject","linkCanvasObjectMode","linkDirectionalArrowLength","linkDirectionalArrowColor","linkDirectionalArrowRelPos","linkDirectionalParticles","linkDirectionalParticleSpeed","linkDirectionalParticleOffset","linkDirectionalParticleWidth","linkDirectionalParticleColor","linkDirectionalParticleCanvasObject","dagMode","dagLevelDistance","dagNodeFilter","onDagError","d3AlphaMin","d3AlphaDecay","d3VelocityDecay","warmupTicks","cooldownTicks","cooldownTime","onEngineTick","onEngineStop"].map(function(i){return Ka({},i,Db.linkProp(i))})).concat(Vn(["nodeRelSize","nodeId","nodeVal","nodeVisibility","linkSource","linkTarget","linkVisibility","linkCurvature"].map(function(i){return Ka({},i,oD.linkProp(i))})))),lD=Object.assign.apply(Object,Vn(["d3Force","d3ReheatSimulation","emitParticle"].map(function(i){return Ka({},i,Db.linkMethod(i))})));function $p(i){if(i.canvas){var e=i.canvas.width,n=i.canvas.height;e===300&&n===150&&(e=n=0);var r=window.devicePixelRatio;e/=r,n/=r,[i.canvas,i.shadowCanvas].forEach(function(o){o.style.width="".concat(i.width,"px"),o.style.height="".concat(i.height,"px"),o.width=i.width*r,o.height=i.height*r,!e&&!n&&o.getContext("2d").scale(r,r)});var s=Cn(i.canvas).k;i.zoom.translateBy(i.zoom.__baseElem,(i.width-e)/2/s,(i.height-n)/2/s),i.needsRedraw=!0}}function Ob(i){var e=window.devicePixelRatio;i.setTransform(e,0,0,e,0,0)}function Rb(i,e,n){i.save(),Ob(i),i.clearRect(0,0,e,n),i.restore()}var Nb=ei({props:ho({width:{default:window.innerWidth,onChange:function(e,n){return $p(n)},triggerUpdate:!1},height:{default:window.innerHeight,onChange:function(e,n){return $p(n)},triggerUpdate:!1},graphData:{default:{nodes:[],links:[]},onChange:function(e,n){[e.nodes,e.links].every(function(s){return(s||[]).every(function(o){return!o.hasOwnProperty("__indexColor")})})&&n.colorTracker.reset(),[{type:"Node",objs:e.nodes},{type:"Link",objs:e.links}].forEach(r),n.forceGraph.graphData(e),n.shadowGraph.graphData(e);function r(s){var o=s.type,a=s.objs;a.filter(function(l){if(!l.hasOwnProperty("__indexColor"))return!0;var c=n.colorTracker.lookup(l.__indexColor);return!c||!c.hasOwnProperty("d")||c.d!==l}).forEach(function(l){l.__indexColor=n.colorTracker.register({type:o,d:l})})}},triggerUpdate:!1},backgroundColor:{onChange:function(e,n){n.canvas&&e&&(n.canvas.style.background=e)},triggerUpdate:!1},nodeLabel:{default:"name",triggerUpdate:!1},nodePointerAreaPaint:{onChange:function(e,n){n.shadowGraph.nodeCanvasObject(e?function(r,s,o){return e(r,r.__indexColor,s,o)}:null),n.flushShadowCanvas&&n.flushShadowCanvas()},triggerUpdate:!1},linkPointerAreaPaint:{onChange:function(e,n){n.shadowGraph.linkCanvasObject(e?function(r,s,o){return e(r,r.__indexColor,s,o)}:null),n.flushShadowCanvas&&n.flushShadowCanvas()},triggerUpdate:!1},linkLabel:{default:"name",triggerUpdate:!1},linkHoverPrecision:{default:4,triggerUpdate:!1},minZoom:{default:.01,onChange:function(e,n){n.zoom.scaleExtent([e,n.zoom.scaleExtent()[1]])},triggerUpdate:!1},maxZoom:{default:1e3,onChange:function(e,n){n.zoom.scaleExtent([n.zoom.scaleExtent()[0],e])},triggerUpdate:!1},enableNodeDrag:{default:!0,triggerUpdate:!1},enableZoomInteraction:{default:!0,triggerUpdate:!1},enablePanInteraction:{default:!0,triggerUpdate:!1},enableZoomPanInteraction:{default:!0,triggerUpdate:!1},enablePointerInteraction:{default:!0,onChange:function(e,n){n.hoverObj=null},triggerUpdate:!1},autoPauseRedraw:{default:!0,triggerUpdate:!1},onNodeDrag:{default:function(){},triggerUpdate:!1},onNodeDragEnd:{default:function(){},triggerUpdate:!1},onNodeClick:{triggerUpdate:!1},onNodeRightClick:{triggerUpdate:!1},onNodeHover:{triggerUpdate:!1},onLinkClick:{triggerUpdate:!1},onLinkRightClick:{triggerUpdate:!1},onLinkHover:{triggerUpdate:!1},onBackgroundClick:{triggerUpdate:!1},onBackgroundRightClick:{triggerUpdate:!1},showPointerCursor:{default:!0,triggerUpdate:!1},onZoom:{triggerUpdate:!1},onZoomEnd:{triggerUpdate:!1},onRenderFramePre:{triggerUpdate:!1},onRenderFramePost:{triggerUpdate:!1}},aD),aliases:{stopAnimation:"pauseAnimation"},methods:ho({graph2ScreenCoords:function(e,n,r){var s=Cn(e.canvas);return{x:n*s.k+s.x,y:r*s.k+s.y}},screen2GraphCoords:function(e,n,r){var s=Cn(e.canvas);return{x:(n-s.x)/s.k,y:(r-s.y)/s.k}},centerAt:function(e,n,r,s){if(!e.canvas)return null;if(n!==void 0||r!==void 0){var o=Object.assign({},n!==void 0?{x:n}:{},r!==void 0?{y:r}:{});return s?e.tweenGroup.add(new oo(a()).to(o,s).easing(si.Quadratic.Out).onUpdate(l).onComplete(function(){e.tweenGroup.remove(this)}).start()):l(o),this}return a();function a(){var c=Cn(e.canvas);return{x:(e.width/2-c.x)/c.k,y:(e.height/2-c.y)/c.k}}function l(c){var u=c.x,h=c.y;e.zoom.translateTo(e.zoom.__baseElem,u===void 0?a().x:u,h===void 0?a().y:h),e.needsRedraw=!0}},zoom:function(e,n,r){if(!e.canvas)return null;if(n!==void 0)return r?e.tweenGroup.add(new oo({k:s()}).to({k:n},r).easing(si.Quadratic.Out).onUpdate(function(a){var l=a.k;return o(l)}).onComplete(function(){e.tweenGroup.remove(this)}).start()):o(n),this;return s();function s(){return Cn(e.canvas).k}function o(a){e.zoom.scaleTo(e.zoom.__baseElem,a),e.needsRedraw=!0}},zoomToFit:function(e){for(var n=arguments.length>1&&arguments[1]!==void 0?arguments[1]:0,r=arguments.length>2&&arguments[2]!==void 0?arguments[2]:10,s=arguments.length,o=new Array(s>3?s-3:0),a=3;a<s;a++)o[a-3]=arguments[a];var l=this.getGraphBbox.apply(this,o);if(l){var c={x:(l.x[0]+l.x[1])/2,y:(l.y[0]+l.y[1])/2},u=Math.max(1e-12,Math.min(1e12,(e.width-r*2)/(l.x[1]-l.x[0]),(e.height-r*2)/(l.y[1]-l.y[0])));this.centerAt(c.x,c.y,n),this.zoom(u,n)}return this},getGraphBbox:function(e){var n=arguments.length>1&&arguments[1]!==void 0?arguments[1]:function(){return!0},r=we(e.nodeVal),s=function(l){return Math.sqrt(Math.max(0,r(l)||1))*e.nodeRelSize},o=e.graphData.nodes.filter(n).map(function(a){return{x:a.x,y:a.y,r:s(a)}});return o.length?{x:[Xr(o,function(a){return a.x-a.r}),Wr(o,function(a){return a.x+a.r})],y:[Xr(o,function(a){return a.y-a.r}),Wr(o,function(a){return a.y+a.r})]}:null},pauseAnimation:function(e){return e.animationFrameRequestId&&(cancelAnimationFrame(e.animationFrameRequestId),e.animationFrameRequestId=null),this},resumeAnimation:function(e){return e.animationFrameRequestId||this._animationCycle(),this},_destructor:function(){this.pauseAnimation(),this.graphData({nodes:[],links:[]})}},lD),stateInit:function(){return{lastSetZoom:1,zoom:Hp(),forceGraph:new Ih,shadowGraph:new Ih().cooldownTicks(0).nodeColor("__indexColor").linkColor("__indexColor").isShadow(!0),colorTracker:new yb,tweenGroup:new Ra}},init:function(e,n){var r=this;e.innerHTML="";var s=document.createElement("div");s.classList.add("force-graph-container"),s.style.position="relative",e.appendChild(s),n.canvas=document.createElement("canvas"),n.backgroundColor&&(n.canvas.style.background=n.backgroundColor),s.appendChild(n.canvas),n.shadowCanvas=document.createElement("canvas");var o=n.canvas.getContext("2d"),a=n.shadowCanvas.getContext("2d",{willReadFrequently:!0}),l={x:-1e12,y:-1e12},c=function(){var d=null,f=window.devicePixelRatio,p=l.x>0&&l.y>0?a.getImageData(l.x*f,l.y*f,1,1):null;return p&&(d=n.colorTracker.lookup(p.data)),d};zt(n.canvas).call(Vp().subject(function(){if(!n.enableNodeDrag)return null;var h=c();return h&&h.type==="Node"?h.d:null}).on("start",function(h){var d=h.subject;d.__initialDragPos={x:d.x,y:d.y,fx:d.fx,fy:d.fy},h.active||(d.fx=d.x,d.fy=d.y),n.canvas.classList.add("grabbable")}).on("drag",function(h){var d=h.subject,f=d.__initialDragPos,p=h,g=Cn(n.canvas).k,v={x:f.x+(p.x-f.x)/g-d.x,y:f.y+(p.y-f.y)/g-d.y};["x","y"].forEach(function(_){return d["f".concat(_)]=d[_]=f[_]+(p[_]-f[_])/g}),!(!d.__dragged&&sD>=Math.sqrt(Su(["x","y"].map(function(_){return Math.pow(h[_]-f[_],2)}))))&&(n.forceGraph.d3AlphaTarget(.3).resetCountdown(),n.isPointerDragging=!0,d.__dragged=!0,n.onNodeDrag(d,v))}).on("end",function(h){var d=h.subject,f=d.__initialDragPos,p={x:d.x-f.x,y:d.y-f.y};f.fx===void 0&&(d.fx=void 0),f.fy===void 0&&(d.fy=void 0),delete d.__initialDragPos,n.forceGraph.d3AlphaTarget()&&n.forceGraph.d3AlphaTarget(0).resetCountdown(),n.canvas.classList.remove("grabbable"),n.isPointerDragging=!1,d.__dragged&&(delete d.__dragged,n.onNodeDragEnd(d,p))})),n.zoom(n.zoom.__baseElem=zt(n.canvas)),n.zoom.__baseElem.on("dblclick.zoom",null),n.zoom.filter(function(h){return!h.button&&n.enableZoomPanInteraction&&(h.type!=="wheel"||we(n.enableZoomInteraction)(h))&&(h.type==="wheel"||we(n.enablePanInteraction)(h))}).on("zoom",function(h){var d=h.transform;[o,a].forEach(function(f){Ob(f),f.translate(d.x,d.y),f.scale(d.k,d.k)}),n.isPointerDragging=!0,n.onZoom&&n.onZoom(ho(ho({},d),r.centerAt())),n.needsRedraw=!0}).on("end",function(h){n.isPointerDragging=!1,n.onZoomEnd&&n.onZoomEnd(ho(ho({},h.transform),r.centerAt()))}),$p(n),n.forceGraph.onNeedsRedraw(function(){return n.needsRedraw=!0}).onFinishUpdate(function(){Cn(n.canvas).k===n.lastSetZoom&&n.graphData.nodes.length&&(n.zoom.scaleTo(n.zoom.__baseElem,n.lastSetZoom=rD/Math.cbrt(n.graphData.nodes.length)),n.needsRedraw=!0)}),n.tooltip=new vh(s),["pointermove","pointerdown"].forEach(function(h){return s.addEventListener(h,function(d){h==="pointerdown"&&(n.isPointerPressed=!0,n.pointerDownEvent=d),!n.isPointerDragging&&d.type==="pointermove"&&n.onBackgroundClick&&(d.pressure>0||n.isPointerPressed)&&(d.pointerType==="mouse"||d.movementX===void 0||[d.movementX,d.movementY].some(function(g){return Math.abs(g)>1}))&&(n.isPointerDragging=!0);var f=p(s);l.x=d.pageX-f.left,l.y=d.pageY-f.top;function p(g){var v=g.getBoundingClientRect(),_=window.pageXOffset||document.documentElement.scrollLeft,m=window.pageYOffset||document.documentElement.scrollTop;return{top:v.top+m,left:v.left+_}}},{passive:!0})}),s.addEventListener("pointerup",function(h){if(n.isPointerPressed){if(n.isPointerPressed=!1,n.isPointerDragging){n.isPointerDragging=!1;return}var d=[h,n.pointerDownEvent];requestAnimationFrame(function(){if(h.button===0)if(n.hoverObj){var f=n["on".concat(n.hoverObj.type,"Click")];f&&f.apply(void 0,[n.hoverObj.d].concat(d))}else n.onBackgroundClick&&n.onBackgroundClick.apply(n,d);if(h.button===2)if(n.hoverObj){var p=n["on".concat(n.hoverObj.type,"RightClick")];p&&p.apply(void 0,[n.hoverObj.d].concat(d))}else n.onBackgroundRightClick&&n.onBackgroundRightClick.apply(n,d)})}},{passive:!0}),s.addEventListener("contextmenu",function(h){return!n.onBackgroundRightClick&&!n.onNodeRightClick&&!n.onLinkRightClick?!0:(h.preventDefault(),!1)}),n.forceGraph(o),n.shadowGraph(a);var u=Wp(function(){Rb(a,n.width,n.height),n.shadowGraph.linkWidth(function(d){return we(n.linkWidth)(d)+n.linkHoverPrecision});var h=Cn(n.canvas);n.shadowGraph.globalScale(h.k).tickFrame()},iD);n.flushShadowCanvas=u.flush,(this._animationCycle=function h(){var d=!n.autoPauseRedraw||!!n.needsRedraw||n.forceGraph.isEngineRunning()||n.graphData.links.some(function(T){return T.__photons&&T.__photons.length});if(n.needsRedraw=!1,n.enablePointerInteraction){var f=n.isPointerDragging?null:c();if(f!==n.hoverObj){var p=n.hoverObj,g=p?p.type:null,v=f?f.type:null;if(g&&g!==v){var _=n["on".concat(g,"Hover")];_&&_(null,p.d)}if(v){var m=n["on".concat(v,"Hover")];m&&m(f.d,g===v?p.d:null)}n.tooltip.content(f&&we(n["".concat(f.type.toLowerCase(),"Label")])(f.d)||null),n.canvas.classList[(f&&n["on".concat(v,"Click")]||!f&&n.onBackgroundClick)&&we(n.showPointerCursor)(f?.d)?"add":"remove"]("clickable"),n.hoverObj=f}d&&u()}if(d){Rb(o,n.width,n.height);var w=Cn(n.canvas).k;n.onRenderFramePre&&n.onRenderFramePre(o,w),n.forceGraph.globalScale(w).tickFrame(),n.onRenderFramePost&&n.onRenderFramePost(o,w)}n.tweenGroup.update(),n.animationFrameRequestId=requestAnimationFrame(h)})()},update:function(e){}});var Ub="ctxt.search-graph/",Lh=["returned","cut_limit","cut_threshold"],Jp=["matched","mentions","extends","supports","contradicts","supersedes","derived-from","related-to","co_mention","similar"],cD=["fts","vector","mention_boost","graph_relevance","word_overlap","total"],Gn=class extends Error{},xi=i=>i!==null&&typeof i=="object"&&!Array.isArray(i);function Fb(i){if(!xi(i)||!xi(i.graph))throw new Gn('Not a JSON Graph Format document: expected a top-level "graph" object.');let e=i.graph,n=xi(e.metadata)?e.metadata.vocabulary:void 0;if(typeof n!="string"||!n.startsWith(Ub)){let r=typeof n=="string"?`"${n}"`:"nothing";throw new Gn(`Not a ctxt search graph: graph.metadata.vocabulary must start with "${Ub}", found ${r}.`)}if(!xi(e.nodes))throw new Gn("Malformed search graph: graph.nodes must be an object keyed by node id.");if(e.edges!==void 0&&!Array.isArray(e.edges))throw new Gn("Malformed search graph: graph.edges must be an array.");return e}function kb(i){let e=Object.entries(i.nodes).map(([o,a])=>{let l=xi(a)&&xi(a.metadata)?a.metadata:{},c=xi(a)&&typeof a.label=="string"&&a.label!==""?a.label:o;return{...l,id:o,label:c,kind:typeof l.kind=="string"?l.kind:"unknown"}}),n=new Set(e.map(o=>o.id)),r=[],s=0;for(let o of i.edges||[]){if(!xi(o)||!n.has(o.source)||!n.has(o.target)){s++;continue}let a=xi(o.metadata)?o.metadata:{};r.push({...a,source:o.source,target:o.target,relation:typeof o.relation=="string"?o.relation:"unknown",directed:typeof o.directed=="boolean"?o.directed:i.directed===!0})}return uD(r),{nodes:e,links:r,dropped:s}}function uD(i){let e=new Map;for(let n of i){let r=n.source<n.target?`${n.source}\0${n.target}`:`${n.target}\0${n.source}`;e.has(r)||e.set(r,[]),e.get(r).push(n)}for(let n of e.values())n.length<2||n.forEach((r,s)=>{let o=Math.ceil(s/2)*.25*(s%2?1:-1);r.curvature=r.source<r.target?o:-o})}var hD={"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"};function fD(i){return String(i).replace(/[&<>"']/g,e=>hD[e])}function bn(i,e=4){return typeof i!="number"||!Number.isFinite(i)?"\u2013":Number.isInteger(i)?String(i):Number(i.toFixed(e)).toString()}function Bb(i){let e=String(i);return/^[A-Za-z0-9._:@%+=,/-]+$/.test(e)?e:`'${e.replace(/'/g,"'\\''")}'`}function zb(i){let e=new Map(Jp.map((n,r)=>[n,r]));return[...i].sort((n,r)=>{let s=e.has(n)?e.get(n):Jp.length,o=e.has(r)?e.get(r):Jp.length;return s-o||(n<r?-1:n>r?1:0)})}function Vb(i){let e=fD,n=[e(i.kind)];i.kind==="object"?(i.stage&&n.push(e(i.stage)),i.rank!==void 0&&n.push(`rank ${e(bn(i.rank))}`),i.legs&&n.push(`legs ${e(i.legs)}`)):i.kind==="entity"&&i.mention_count!==void 0&&n.push(`${e(bn(i.mention_count))} mentions`);let r="";return xi(i.score)&&(r=cD.map(s=>`<tr${s==="total"?' class="total"':""}><th>${e(s)}</th><td>${e(bn(i.score[s]))}</td></tr>`).join(""),r=`<table>${r}</table>`),`<div class="tt"><div class="tt-label">${e(i.label)}</div><div class="tt-meta">${n.join(" \xB7 ")}</div>${r}</div>`}var Gb={light:{background:"#f8fafc",kind:{query:"#e11d48",entity:"#8b5cf6",object:"#2563eb",unknown:"#64748b"},stage:{returned:"#2563eb",cut_limit:"#d97706",cut_threshold:"#94a3b8"},relation:{matched:"#94a3b8",mentions:"#a78bfa",extends:"#0284c7",supports:"#059669",contradicts:"#dc2626",supersedes:"#ea580c","derived-from":"#4f46e5","related-to":"#475569",co_mention:"#0d9488",similar:"#ca8a04"},fallback:"#64748b"},dark:{background:"#0b1120",kind:{query:"#fb7185",entity:"#a78bfa",object:"#60a5fa",unknown:"#94a3b8"},stage:{returned:"#60a5fa",cut_limit:"#fbbf24",cut_threshold:"#64748b"},relation:{matched:"#475569",mentions:"#8b5cf6",extends:"#38bdf8",supports:"#34d399",contradicts:"#f87171",supersedes:"#fb923c","derived-from":"#818cf8","related-to":"#94a3b8",co_mention:"#2dd4bf",similar:"#facc15"},fallback:"#94a3b8"}},dD={matched:{width:.4,dash:null},mentions:{width:.6,dash:[2,2]},co_mention:{width:1,dash:[4,2]},similar:{width:1,dash:[1,2]}},pD={width:1.6,dash:null},mD={returned:"returned",cut_limit:"cut by limit",cut_threshold:"cut by threshold"},pt=i=>document.getElementById(i),is=window.matchMedia?window.matchMedia("(prefers-color-scheme: dark)"):null,bi=()=>is&&is.matches?Gb.dark:Gb.light;function Hb(i){let e=pt("error");e.textContent=i,e.hidden=!1}function gD(i){let e=pt("notice");e.textContent=i,e.hidden=!1}async function _D(){let i=pt("graph-data");if(i)try{return JSON.parse(i.textContent)}catch(n){throw new Gn(`Embedded graph data is not valid JSON: ${n.message}`)}let e;try{e=await fetch("graph.json",{cache:"no-store"})}catch(n){throw new Gn(`No embedded graph data, and graph.json could not be fetched: ${n.message}`)}if(!e.ok)throw new Gn(`No embedded graph data, and graph.json returned HTTP ${e.status}.`);try{return await e.json()}catch(n){throw new Gn(`graph.json is not valid JSON: ${n.message}`)}}function vD(){try{let i=document.createElement("canvas");return!!(i.getContext("webgl2")||i.getContext("webgl"))}catch{return!1}}var Oh=class extends Error{},em=i=>i&&i.message?i.message:String(i);function yD(i,e){let n="WebGL is unavailable";if(vD()){let r;try{return r=new kx(i),e(r,3),{graph:r,dims:3}}catch(s){n=`3D renderer failed (${em(s)})`;try{r&&r._destructor()}catch{}i.replaceChildren()}}try{let r=new Nb(i);return e(r,2),gD(`2D mode: ${n}.`),{graph:r,dims:2}}catch(r){throw i.replaceChildren(),new Oh(`This browser could not start the graph renderer. ${n}; the 2D canvas renderer failed too (${em(r)}).`)}}function xD(i,e){let n=i.metadata||{};pt("query").textContent=typeof n.query=="string"?n.query:typeof i.label=="string"?i.label:"";let r=n.counts&&typeof n.counts=="object"?n.counts:{},s=[];n.fts_pool!==void 0&&s.push(`fts ${bn(n.fts_pool)}`),n.vector_pool!==void 0&&s.push(`vector ${bn(n.vector_pool)}`);let o=e.nodes.filter(u=>u.kind==="entity").length,a=[["mode",n.mode],["pools",s.join(" \xB7 ")],["threshold",n.threshold!==void 0?bn(n.threshold):void 0],["limit",n.limit!==void 0?bn(n.limit):void 0],["candidates",r.candidates!==void 0?bn(r.candidates):void 0],["stages",Lh.filter(u=>r[u]!==void 0).map(u=>`${bn(r[u])} ${u}`).join(" \xB7 ")],["nodes",bn(r.nodes!==void 0?r.nodes:e.nodes.length)],["edges",bn(r.edges!==void 0?r.edges:e.links.length)],["entities",bn(r.entities!==void 0?r.entities:o)]],l=pt("facts");for(let[u,h]of a){if(h===void 0||h==="")continue;let d=document.createElement("div"),f=document.createElement("dt"),p=document.createElement("dd");f.textContent=u,p.textContent=String(h),d.append(f,p),l.append(d)}let c=[];n.truncated===!0&&c.push(["truncated","node or edge cap reached; graph is partial"]),typeof n.vector_error=="string"&&n.vector_error&&c.push(["vector error",n.vector_error]),e.dropped>0&&c.push([`${e.dropped} edges skipped`,"edge endpoints missing from nodes"]);for(let[u,h]of c){let d=document.createElement("div"),f=document.createElement("dd");f.className="badge",f.textContent=u,f.title=h,d.append(f),l.append(d)}}function Dh(i,{label:e,count:n,swatch:r,checked:s,onChange:o}){let a=document.createElement("label"),l=document.createElement("input");l.type="checkbox",l.checked=s,l.addEventListener("change",()=>o(l.checked));let c=document.createElement("span");c.className=r.className,r.el=c;let u=document.createElement("span");u.textContent=e;let h=document.createElement("span");return h.className="count",h.textContent=String(n),a.append(l,c,u,h),i.append(a),a}function bD(i){let e=Fb(i),n=kb(e);xD(e,n);let r={stages:{},relations:{},kinds:{query:!0,object:!0,entity:!0}},s=new Map(n.nodes.map(P=>[P.id,P])),o=P=>typeof P=="object"&&P!==null?P:s.get(P),a=P=>r.kinds[P.kind]!==!1&&(P.kind!=="object"||r.stages[P.stage]!==!1),l=P=>{if(r.relations[P.relation]===!1)return!1;let O=o(P.source),U=o(P.target);return!!O&&!!U&&a(O)&&a(U)},c=P=>{let O=bi();return P.kind==="object"&&O.stage[P.stage]?O.stage[P.stage]:O.kind[P.kind]||O.fallback},u=P=>bi().relation[P.relation]||bi().fallback,h=P=>dD[P.relation]||pD,d=n.nodes.reduce((P,O)=>Math.max(P,Wb(O)),0),f=P=>P.kind==="query"?12:P.kind==="entity"?1.5:1+(d>0?9*Wb(P)/d:0),p=pt("graph"),g=!1,v=(P,O)=>{for(let U of n.nodes)U.kind==="query"&&(U.fx=0,U.fy=0,O===3&&(U.fz=0));P.width(p.clientWidth||window.innerWidth).height(p.clientHeight||window.innerHeight).backgroundColor(bi().background).nodeId("id").nodeVal(f).nodeRelSize(4).nodeColor(c).nodeLabel(Vb).nodeVisibility(a).linkVisibility(l).linkColor(u).linkWidth(U=>h(U).width).linkCurvature(U=>U.curvature||0).linkDirectionalArrowLength(U=>U.directed?O===3?3:4:0).linkDirectionalArrowRelPos(1).onNodeClick(SD).warmupTicks(80).cooldownTime(4e3).onEngineStop(()=>{g||(g=!0,P.zoomToFit(400,90))}),O===2&&P.linkLineDash(U=>h(U).dash),P.graphData({nodes:n.nodes,links:n.links})},{graph:_,dims:m}=yD(p,v);document.body.dataset.renderer=`${m}d`,m===2&&(pt("hint").textContent="Drag to pan, scroll to zoom, click an object for its id.");let w=()=>{_.nodeVisibility(a).linkVisibility(l)},T=()=>{_.backgroundColor(bi().background).nodeColor(c).linkColor(u),S()},y=[],b=(P,O)=>y.push({sw:P,color:O}),S=()=>{for(let{sw:P,color:O}of y){let U=O();P.el.style.background=U,P.el.style.borderColor=U}},A=Qp(n.nodes,P=>P.kind);for(let P of["query","object","entity"]){if(!A.has(P))continue;let O={className:"swatch"};if(b(O,()=>bi().kind[P]),P==="query"){let U=Dh(pt("kinds"),{label:P,count:A.get(P),swatch:O,checked:!0,onChange:()=>{}});U.querySelector("input").disabled=!0;continue}Dh(pt("kinds"),{label:P==="object"?"objects":"entities",count:A.get(P),swatch:O,checked:!0,onChange:U=>{r.kinds[P]=U,w()}})}let x=Qp(n.nodes.filter(P=>P.kind==="object"),P=>P.stage),C=[...Lh,...[...x.keys()].filter(P=>typeof P=="string"&&!Lh.includes(P)).sort()];for(let P of C){let O={className:"swatch"};b(O,()=>bi().stage[P]||bi().kind.object),Dh(pt("stages"),{label:mD[P]||String(P),count:x.get(P)||0,swatch:O,checked:!0,onChange:U=>{r.stages[P]=U,w()}})}let L=Qp(n.links,P=>P.relation);for(let P of zb(L.keys())){let O={className:`swatch line${h({relation:P}).dash?" dashed":""}`};b(O,()=>bi().relation[P]||bi().fallback),Dh(pt("relations"),{label:P,count:L.get(P),swatch:O,checked:!0,onChange:U=>{r.relations[P]=U,w()}})}if(S(),is){let P=()=>T();is.addEventListener?is.addEventListener("change",P):is.addListener&&is.addListener(P)}window.addEventListener("resize",()=>{_.width(p.clientWidth||window.innerWidth).height(p.clientHeight||window.innerHeight)}),window.ctxtSearchGraph={graph:_,data:n,dims:m}}function Wb(i){let e=i.score&&i.score.total;return typeof e=="number"&&Number.isFinite(e)&&e>0?e:0}function Qp(i,e){let n=new Map;for(let r of i){let s=e(r);n.set(s,(n.get(s)||0)+1)}return n}function SD(i){if(!i)return;pt("details-kind").textContent=[i.kind,i.stage,i.rank!==void 0?`rank ${bn(i.rank)}`:""].filter(Boolean).join(" \xB7 "),pt("details-label").textContent=i.label;let e=pt("details-object");i.kind==="object"&&typeof i.object_id=="string"&&i.object_id!==""?(pt("details-id").textContent=i.object_id,pt("details-cmd").textContent=`ctxt show ${Bb(i.object_id)}`,pt("details-copied").textContent="",e.hidden=!1):e.hidden=!0,pt("details").hidden=!1,(e.hidden?pt("details-close"):pt("details-copy")).focus({preventScroll:!0})}async function wD(){let i=pt("details-cmd").textContent,e=pt("details-copied");try{await navigator.clipboard.writeText(i),e.textContent="Copied"}catch{let n=document.createRange();n.selectNodeContents(pt("details-cmd"));let r=window.getSelection();r.removeAllRanges(),r.addRange(n),e.textContent="Selected: press Ctrl/Cmd+C"}}function Xb(){pt("details").hidden=!0}function qb(){pt("details-close").addEventListener("click",Xb),pt("details-copy").addEventListener("click",wD),document.addEventListener("keydown",i=>{i.key==="Escape"&&Xb()}),_D().then(bD).catch(i=>{i instanceof Gn||i instanceof Oh?Hb(i.message):(Hb(`The search graph could not be rendered: ${em(i)}`),console.error(i))})}document.readyState==="loading"?document.addEventListener("DOMContentLoaded",qb):qb();})();
/*! For license information please see viewer.js.LEGAL.txt */
